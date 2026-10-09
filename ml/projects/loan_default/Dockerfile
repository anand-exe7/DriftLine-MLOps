# Offline training image: python train.py --no-publish writes onnx/ bundles,
# artifacts/ (metrics, baseline, SHAP) and weights/ inside /app.
FROM python:3.12-slim

WORKDIR /app

COPY requirements.txt .

RUN pip install --no-cache-dir -r requirements.txt

COPY . .

CMD ["python", "train.py", "--no-publish"]
