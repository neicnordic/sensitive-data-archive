FROM python:3.11-slim

COPY .github/integration/sda/oidc.py .
RUN pip install --upgrade pip && pip install aiohttp Authlib joserfc requests && sed -i 's/localhost/oidc/g' oidc.py

CMD [ "python", "-u", "/oidc.py" ]