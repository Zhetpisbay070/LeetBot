
# How to launch system locally

1. Define .env file inside the `docker/` folder

2. Launch the db
```bash
docker-compose -f docker/mongo.yaml up -d
```

3. Launch the app
```bash
docker-compose -f docker/app.yml up -d --build
```

```bash
docker-compose -f docker/app.yml up -d --build app
```