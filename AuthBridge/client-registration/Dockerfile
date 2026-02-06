# Use minimal Python base image
FROM python:3.12-slim

# Create non-root user
RUN groupadd -g 1000 appuser && useradd -u 1000 -g appuser appuser

# Set work directory
WORKDIR /app

# Copy your script
COPY client_registration.py .

# Install dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Ensure non-root user owns application directory
RUN chown -R 1000:1000 /app
# Switch to non-root user
USER 1000:1000

# Register client
CMD ["python", "client_registration.py"]