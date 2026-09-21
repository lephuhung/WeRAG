import gc
import torch
import uvicorn
from fastapi import FastAPI
from pydantic import BaseModel, Field
from transformers import AutoModelForSequenceClassification, AutoTokenizer
from typing import List

# Enable CUDA debugging
# import os
# os.environ['CUDA_LAUNCH_BLOCKING']='1'

# --- 1. Define API request and response data structures ---

# Request body structure remains unchanged
class RerankRequest(BaseModel):
    query: str
    documents: List[str]

# --- Modification start: Define response structure for testing, field name "score" ---

# DocumentInfo structure remains unchanged
class DocumentInfo(BaseModel):
    text: str

# Modify original GoRankResult to TestRankResult
# Core change: rename "relevance_score" field to "score"
class TestRankResult(BaseModel):
    index: int
    document: DocumentInfo
    score: float  # <--- [Key modification] Field name changed from relevance_score to score

# Final response body structure, whose "results" list contains TestRankResult
class TestFinalResponse(BaseModel):
    results: List[TestRankResult]

# --- Modification end ---


# --- 2. Load model (executed once at service startup) ---
print("Loading model, please wait...")
device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
print(f"Using device: {device}")
try:
    # Please ensure the path here is correct
    model_path = '/data1/home/lwx/work/Download/rerank_model_weight'
    tokenizer = AutoTokenizer.from_pretrained(model_path)
    model = AutoModelForSequenceClassification.from_pretrained(model_path)
    model.to(device)
    model.eval()
    print("Model loaded successfully!")
except Exception as e:
    print(f"Model load failed: {e}")
    # In test environment, if model loading fails, consider exiting to avoid running an invalid service
    exit()

# --- 3. Create FastAPI app ---
app = FastAPI(
    title="Reranker API (Test Version)",
    description="An API service returning 'score' field to test Go client compatibility",
    version="1.0.2"
)

# --- 4. Define API endpoints ---
# --- Modification start: Point response_model to new test response structure ---
@app.post("/rerank", response_model=TestFinalResponse) # <--- [Key modification] response_model changed to TestFinalResponse
def rerank_endpoint(request: RerankRequest):
    # --- Modification end ---

    pairs = [[request.query, doc] for doc in request.documents]

    with torch.no_grad():
        inputs = outputs = logits = None

        try:
            inputs = tokenizer(pairs, padding=True, truncation=True, return_tensors='pt', max_length=1024).to(device)
            outputs = model(**inputs, return_dict=True)
            logits = outputs.logits.view(-1, ).float()
            scores = torch.sigmoid(logits)
        finally:
            # Release GPU resource usage
            del inputs, outputs, logits
            gc.collect()

            if torch.cuda.is_available():
                torch.cuda.empty_cache()
            elif hasattr(torch, "mps") and torch.mps.is_available():
                torch.mps.empty_cache()


    # --- Modification start: Build result according to test structure ---
    results = []
    for i, (text, score_val) in enumerate(zip(request.documents, scores)):
        
        # 1. Create nested document object
        doc_info = DocumentInfo(text=text)
        
        # 2. Create TestRankResult object
        #    Note field names: index, document, score
        test_result = TestRankResult(
            index=i,
            document=doc_info,
            score=score_val.item()  # <--- [Key modification] Assign to "score" field
        )
        results.append(test_result)

    # 3. Sort (key also modified to score accordingly)
    sorted_results = sorted(results, key=lambda x: x.score, reverse=True)
    # --- Modification end ---
    
    # Return a dictionary, FastAPI will validate and serialize it according to response_model (TestFinalResponse)
    # Final generated JSON will be {"results": [{"index": ..., "document": ..., "score": ...}]}
    return {"results": sorted_results}

@app.get("/")
def read_root():
    return {"status": "Reranker API (Test Version) is running"}

# --- 5. Start service ---
if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8000)

