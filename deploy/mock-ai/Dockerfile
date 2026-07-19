FROM python:3.14.3-alpine3.23

RUN addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app app

WORKDIR /app
COPY --chown=app:app server.py smoke.py /app/

USER 10001:10001
EXPOSE 8080

CMD ["python3", "/app/server.py"]
