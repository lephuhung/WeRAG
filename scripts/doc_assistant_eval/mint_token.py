#!/usr/bin/env python3
"""Mint (or revoke) an access token for a test user without going through the
login form (useful when the account has no known password or 2FA).

The backend validates a JWT (HS256, JWT_SECRET) *and* requires the exact token
string to exist in auth_tokens. So this script signs the token and inserts the
row with psql. Example (dev stack, docker):

    JWT_SECRET=$(sudo -n docker exec WeKnora-app-dev sh -c 'echo $JWT_SECRET') \
    python3 mint_token.py --email claude_admin@test.local --tenant 10003 \
        --psql "sudo -n docker exec -i WeKnora-postgres psql -U postgres -d WeKnora -v ON_ERROR_STOP=1"

Prints the token on stdout. `--revoke TOKEN` marks it revoked again.
"""
from __future__ import annotations

import argparse
import base64
import datetime as dt
import hashlib
import hmac
import json
import os
import subprocess
import sys
import uuid


def b64(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode()


def sign(claims: dict, secret: str) -> str:
    header = b64(json.dumps({"alg": "HS256", "typ": "JWT"}, separators=(",", ":")).encode())
    payload = b64(json.dumps(claims, separators=(",", ":")).encode())
    sig = hmac.new(secret.encode(), f"{header}.{payload}".encode(), hashlib.sha256).digest()
    return f"{header}.{payload}.{b64(sig)}"


def psql(cmd: str, sql: str) -> str:
    p = subprocess.run(cmd, shell=True, input=sql.encode(), capture_output=True)
    if p.returncode != 0:
        sys.stderr.write(p.stderr.decode())
        raise SystemExit(f"psql failed ({p.returncode})")
    return p.stdout.decode()


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--email", default="claude_admin@test.local")
    ap.add_argument("--tenant", type=int, default=10003)
    ap.add_argument("--hours", type=int, default=12)
    ap.add_argument("--secret", default=os.environ.get("JWT_SECRET", ""))
    ap.add_argument("--psql", default=os.environ.get("WERAG_PSQL", "sudo -n docker exec -i WeKnora-postgres psql -U postgres -d WeKnora -v ON_ERROR_STOP=1"),
                    help="shell command that runs psql reading SQL from stdin")
    ap.add_argument("--revoke", metavar="TOKEN", help="revoke this token instead of minting")
    args = ap.parse_args()

    if args.revoke:
        psql(args.psql, f"update auth_tokens set is_revoked=true, updated_at=now() where token='{args.revoke}';")
        print("revoked", file=sys.stderr)
        return

    if not args.secret:
        raise SystemExit("JWT_SECRET missing (--secret or env)")
    out = psql(args.psql + " -tA", f"select id from users where email='{args.email}' and deleted_at is null limit 1;")
    user_id = out.strip()
    if not user_id:
        raise SystemExit(f"no user with email {args.email}")

    now = dt.datetime.now(dt.timezone.utc)
    exp = now + dt.timedelta(hours=args.hours)
    claims = {"user_id": user_id, "email": args.email, "tenant_id": args.tenant,
              "exp": int(exp.timestamp()), "iat": int(now.timestamp()), "type": "access"}
    token = sign(claims, args.secret)
    row_id = str(uuid.uuid4())
    psql(args.psql, (
        "insert into auth_tokens (id, user_id, token, token_type, expires_at, is_revoked, created_at, updated_at) "
        f"values ('{row_id}', '{user_id}', '{token}', 'access_token', '{exp.isoformat()}', false, now(), now());"
    ))
    print(token)


if __name__ == "__main__":
    main()
