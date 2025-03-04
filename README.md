# Distributed Multiplayer Pac-Man


## How to run

### General requirements

1. Install golang
2. Configure Docker - use docker login on command line, and create a .env file in the top level containing DOCKER_USERNAME=\<your-username\>
3. Install kind for local Kubernetes development.

### Start central server locally

First, if you have updated any of the code, run the following
```
central_server/new-image.sh
```

Next, to bring up the central cluster (including mongodb and the server) itself
```
./central_server/setup.sh
```

Then, the server is accessible on localhost:8080. For example, sending a GET request to localhost:8080/dbconn returns that the server is able to connect to MongoDB.

If you want to access MongoDB from outside the cluster (eg if you want to view the database contents through a software like MongoDB Compass), run the following command:
```
kubectl port-forward --address 0.0.0.0 svc/mongodb 27017:27017
```

When shutting the system down, run the following. If you have updated any code and want to deploy it to your local cluster, run this before repeating the steps above.
```
./cleanup.sh
```

### Start chunk server locally

First, if you have updated any of the code, run the following
```
chunk_server/new-image.sh
```

Next, to bring up the chunk clusters. By default this brings up 3 clusters, each with 1 server inside to start with.
```
./chunk_server/setup.sh
```

Then, each server is accessible on localhost:8081, localhost:8082, and localhost:8083. 

When shutting the system down, run the following. If you have updated any code and want to deploy it to your local cluster, run this before repeating the steps above.
```
./cleanup.sh
```

### Start client locally

Follow instructions in the [frontend's README](frontend/README.md).