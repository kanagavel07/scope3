# Project Name: Scope3

## Environment Variables

To configure the application, you need to create a `.env` file in the project directory with the following content:

```env
SCOPE3_API_TOKEN=your_api_token_here
```

Replace `your_api_token_here` with your actual API token.

Make sure to keep this file secure and do not expose it in version control.

## Instructions to Build and Run with Docker

### Build the Docker Image

To build the Docker image, run the following command in the project directory:

```sh
sudo docker build -f Dockerfile.multistage -t scope3:multistage .
```

### Run the Docker Container

To run the Docker container, use the following command:

```sh
sudo docker run --publish 8080:8080 scope3:multistage
```

### Sample `curl` POST Commands

To test the application, you can use the following `curl` commands to send POST requests and measure the time taken:
The first request will be slow and the second request with same body will take less than 50ms

```sh
time curl -X POST http://localhost:8080/measure -d '{"rows": [{"inventoryId": "nytimes.com","utcDatetime": "2024-12-30"},{"inventoryId": "yahoo.com","utcDatetime": "2024-12-30"}]}' -H "Content-Type: application/json"
```

```sh
time curl -X POST http://localhost:8080/measure -d '{"rows": [{"inventoryId": "nytimes.com","utcDatetime": "2024-12-30"},{"inventoryId": "yahoo.com","utcDatetime": "2024-12-30"}]}' -H "Content-Type: application/json"
```

### Run Tests with Docker

To run the tests using Docker, use the following command:

```sh
sudo docker build -f Dockerfile.multistage -t scope3-test:multistage --progress plain --no-cache --target run-test-stage .
```
