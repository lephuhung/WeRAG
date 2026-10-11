"""LoRA fine-tune of a Qwen3 chat model on build_dataset.py output.

The prompt is rendered with the tokenizer's chat template exactly as vLLM
renders it at inference (enable_thinking=False), and only the assistant JSON
is trained on. Run with the repo's system torch/transformers plus peft and
accelerate (see README).

  python train_lora.py --model models/Qwen3-1.7B --data data/sft --out runs/pilot
  python train_lora.py ... --merge   # also writes a merged model for vLLM
"""
from __future__ import annotations

import argparse
import json
import math
import sys
import time
from pathlib import Path

import torch
from peft import LoraConfig, get_peft_model
from torch.utils.data import DataLoader, Dataset
from transformers import AutoModelForCausalLM, AutoTokenizer, get_cosine_schedule_with_warmup


class SftDataset(Dataset):
    def __init__(self, path: str, tok, max_len: int):
        self.items = []
        dropped = 0
        for line in open(path, encoding="utf-8"):
            m = json.loads(line)["messages"]
            prompt = tok.apply_chat_template(m[:2], tokenize=False, add_generation_prompt=True, enable_thinking=False)
            full = tok.apply_chat_template(m, tokenize=False, enable_thinking=False)
            if not full.startswith(prompt):
                full = prompt + m[2]["content"] + "<|im_end|>\n"
            p_ids = tok(prompt, add_special_tokens=False)["input_ids"]
            f_ids = tok(full, add_special_tokens=False)["input_ids"]
            if len(f_ids) > max_len or f_ids[: len(p_ids)] != p_ids:
                dropped += 1
                continue
            labels = [-100] * len(p_ids) + f_ids[len(p_ids) :]
            self.items.append((f_ids, labels))
        print(f"{path}: {len(self.items)} samples, dropped {dropped} (too long or template mismatch)", file=sys.stderr)

    def __len__(self):
        return len(self.items)

    def __getitem__(self, i):
        return self.items[i]


def collate(batch, pad_id: int):
    n = max(len(x[0]) for x in batch)
    ids = torch.full((len(batch), n), pad_id, dtype=torch.long)
    lab = torch.full((len(batch), n), -100, dtype=torch.long)
    att = torch.zeros((len(batch), n), dtype=torch.long)
    for i, (f, l) in enumerate(batch):
        ids[i, : len(f)] = torch.tensor(f)
        lab[i, : len(l)] = torch.tensor(l)
        att[i, : len(f)] = 1
    return ids, att, lab


def length_grouped_batches(ds: SftDataset, bs: int, seed: int):
    """Shuffle, then sort inside windows so padding stays small."""
    g = torch.Generator().manual_seed(seed)
    idx = torch.randperm(len(ds), generator=g).tolist()
    window = bs * 50
    batches = []
    for s in range(0, len(idx), window):
        w = sorted(idx[s : s + window], key=lambda i: len(ds.items[i][0]))
        batches.extend(w[i : i + bs] for i in range(0, len(w), bs))
    order = torch.randperm(len(batches), generator=g).tolist()
    return [batches[i] for i in order]


@torch.no_grad()
def evaluate(model, ds, pad_id, bs):
    model.eval()
    tot, n = 0.0, 0
    for s in range(0, len(ds), bs):
        ids, att, lab = collate([ds[i] for i in range(s, min(len(ds), s + bs))], pad_id)
        out = model(input_ids=ids.cuda(), attention_mask=att.cuda(), labels=lab.cuda())
        k = (lab != -100).sum().item()
        tot += out.loss.item() * k
        n += k
    model.train()
    return tot / max(1, n)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", required=True)
    ap.add_argument("--data", required=True, help="dir with train.jsonl and val.jsonl")
    ap.add_argument("--out", required=True)
    ap.add_argument("--max-len", type=int, default=2048)
    ap.add_argument("--epochs", type=float, default=1.0)
    ap.add_argument("--bs", type=int, default=8)
    ap.add_argument("--grad-accum", type=int, default=4)
    ap.add_argument("--lr", type=float, default=2e-4)
    ap.add_argument("--rank", type=int, default=32)
    ap.add_argument("--alpha", type=int, default=64)
    ap.add_argument("--dropout", type=float, default=0.05)
    ap.add_argument("--warmup", type=float, default=0.03)
    ap.add_argument("--eval-every", type=int, default=200)
    ap.add_argument("--log-every", type=int, default=20)
    ap.add_argument("--seed", type=int, default=0)
    ap.add_argument("--merge", action="store_true", help="also save a merged full model under out/merged")
    ap.add_argument("--no-grad-ckpt", action="store_true", help="skip gradient checkpointing (faster, needs more VRAM)")
    ap.add_argument("--save-every", type=int, default=100, help="write a resumable checkpoint (adapter + optimizer) every N optimizer steps")
    ap.add_argument("--resume", action="store_true", help="continue from out/ckpt if it exists")
    ap.add_argument("--init-adapter", help="start from this LoRA adapter (weights only, fresh optimizer and schedule)")
    a = ap.parse_args()
    torch.manual_seed(a.seed)
    out = Path(a.out)
    out.mkdir(parents=True, exist_ok=True)

    tok = AutoTokenizer.from_pretrained(a.model)
    pad_id = tok.pad_token_id if tok.pad_token_id is not None else tok.eos_token_id
    train = SftDataset(str(Path(a.data) / "train.jsonl"), tok, a.max_len)
    val = SftDataset(str(Path(a.data) / "val.jsonl"), tok, a.max_len)

    try:
        model = AutoModelForCausalLM.from_pretrained(a.model, dtype=torch.bfloat16, attn_implementation="sdpa").cuda()
    except Exception as e:  # multimodal checkpoints (Gemma 4, Qwen3.5) load through the image-text class
        print(f"AutoModelForCausalLM failed ({type(e).__name__}); using AutoModelForImageTextToText", file=sys.stderr)
        from transformers import AutoModelForImageTextToText

        model = AutoModelForImageTextToText.from_pretrained(a.model, dtype=torch.bfloat16, attn_implementation="sdpa").cuda()
    if not a.no_grad_ckpt:
        model.gradient_checkpointing_enable(gradient_checkpointing_kwargs={"use_reentrant": False})
    model.config.use_cache = False
    # Explicit module list: every nn.Linear projection of the language model,
    # skipping vision/audio towers of multimodal checkpoints (Gemma 4, Qwen3.5).
    wanted = ("q_proj", "k_proj", "v_proj", "o_proj", "gate_proj", "up_proj", "down_proj")
    targets = [
        n
        for n, mod in model.named_modules()
        if isinstance(mod, torch.nn.Linear)
        and n.rsplit(".", 1)[-1] in wanted
        and not any(t in n for t in ("vision", "visual", "audio", "embed_vision", "embed_audio"))
    ]
    print(f"lora targets: {len(targets)} linear layers", file=sys.stderr)
    cfg = LoraConfig(
        r=a.rank,
        lora_alpha=a.alpha,
        lora_dropout=a.dropout,
        bias="none",
        task_type="CAUSAL_LM",
        target_modules=targets,
    )
    ckpt = out / "ckpt"
    resume_state = None
    if a.resume and (ckpt / "state.json").exists():
        from peft import PeftModel

        model = PeftModel.from_pretrained(model, str(ckpt / "adapter"), is_trainable=True)
        resume_state = json.loads((ckpt / "state.json").read_text())
        print(f"resuming from step {resume_state['step']}", file=sys.stderr)
    elif a.init_adapter:
        from peft import PeftModel

        model = PeftModel.from_pretrained(model, a.init_adapter, is_trainable=True)
        print(f"initialised from adapter {a.init_adapter}", file=sys.stderr)
    else:
        model = get_peft_model(model, cfg)
    model.enable_input_require_grads()
    model.print_trainable_parameters()

    batches = length_grouped_batches(train, a.bs, a.seed)
    steps_per_epoch = math.ceil(len(batches) / a.grad_accum)
    total_steps = max(1, int(steps_per_epoch * a.epochs))
    opt = torch.optim.AdamW([p for p in model.parameters() if p.requires_grad], lr=a.lr, weight_decay=0.0, betas=(0.9, 0.999))
    sched = get_cosine_schedule_with_warmup(opt, int(total_steps * a.warmup), total_steps)
    print(f"train samples={len(train)} micro-batches={len(batches)} optimizer steps={total_steps}", file=sys.stderr)

    model.train()
    step, micro, t0 = 0, 0, time.time()
    run_loss, run_tok = 0.0, 0
    log = open(out / "train_log.jsonl", "a", encoding="utf-8")
    epoch = 0
    if resume_state:
        step, micro, epoch = resume_state["step"], resume_state["micro"], resume_state["epoch"]
        t0 = time.time() - resume_state.get("sec", 0)
        opt.load_state_dict(torch.load(ckpt / "optimizer.pt", map_location="cuda"))
        for _ in range(step):
            sched.step()
        for _ in range(epoch):
            batches = length_grouped_batches(train, a.bs, a.seed + epoch)

    def save_ckpt():
        tmp = out / "ckpt_tmp"
        if tmp.exists():
            import shutil

            shutil.rmtree(tmp)
        tmp.mkdir(parents=True)
        model.save_pretrained(tmp / "adapter")
        torch.save(opt.state_dict(), tmp / "optimizer.pt")
        (tmp / "state.json").write_text(json.dumps({"step": step, "micro": micro, "epoch": epoch, "sec": round(time.time() - t0)}))
        if ckpt.exists():
            import shutil

            shutil.rmtree(ckpt)
        tmp.rename(ckpt)
    while step < total_steps:
        if micro >= len(batches):
            epoch += 1
            batches = length_grouped_batches(train, a.bs, a.seed + epoch)
            micro = 0
        ids, att, lab = collate([train[i] for i in batches[micro]], pad_id)
        micro += 1
        o = model(input_ids=ids.cuda(), attention_mask=att.cuda(), labels=lab.cuda())
        (o.loss / a.grad_accum).backward()
        run_loss += o.loss.item() * (lab != -100).sum().item()
        run_tok += (lab != -100).sum().item()
        if micro % a.grad_accum == 0:
            torch.nn.utils.clip_grad_norm_(model.parameters(), 1.0)
            opt.step()
            sched.step()
            opt.zero_grad(set_to_none=True)
            step += 1
            if step % a.log_every == 0 or step == total_steps:
                rec = {"step": step, "loss": round(run_loss / max(1, run_tok), 4), "lr": sched.get_last_lr()[0], "sec": round(time.time() - t0)}
                run_loss, run_tok = 0.0, 0
                if step % a.eval_every == 0 or step == total_steps:
                    rec["val_loss"] = round(evaluate(model, val, pad_id, a.bs), 4)
                print(json.dumps(rec), file=sys.stderr)
                log.write(json.dumps(rec) + "\n")
                log.flush()
            if a.save_every and step % a.save_every == 0:
                save_ckpt()
    model.save_pretrained(out / "adapter")
    tok.save_pretrained(out / "adapter")
    if a.merge:
        merged = model.merge_and_unload()
        merged.save_pretrained(out / "merged", safe_serialization=True)
        tok.save_pretrained(out / "merged")
    print(f"saved to {out}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
