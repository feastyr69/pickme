from contextlib import asynccontextmanager
import cv2
import httpx
import numpy as np
from fastapi import FastAPI, HTTPException
from insightface.app import FaceAnalysis
from pydantic import BaseModel

face_app = None

@asynccontextmanager
async def lifespan(app: FastAPI):
    global face_app
    face_app = FaceAnalysis(name="buffalo_l", providers=["CPUExecutionProvider"])
    face_app.prepare(ctx_id=-1, det_size=(640, 640))
    yield

app = FastAPI(lifespan=lifespan)

class ExtractRequest(BaseModel):
    image_url: str

@app.get("/health")
async def health_check():
    return {"status": "healthy"}

@app.post("/extract")
async def extract_faces(request: ExtractRequest):
    async with httpx.AsyncClient(timeout=10.0) as client:   
        resp = await client.get(request.image_url)
        if resp.status_code != 200:
            raise HTTPException(status_code=resp.status_code, detail="Failed to fetch image from URL")

        img_array = np.frombuffer(resp.content, np.uint8)
        img = cv2.imdecode(img_array, cv2.IMREAD_COLOR)
        if img is None:
            raise HTTPException(status_code=400, detail="Failed to decode image")
        
        faces = face_app.get(img)

        results = []
        for face in faces:
            results.append({
                "bbox": face.bbox.astype(int).tolist(),
                "det_score": float(face.det_score),
                "embedding": face.normed_embedding.tolist(),
            })

        return {"faces": results}

