up:
	docker compose -f backend/deployments/docker-compose.yaml up

buildup:
	docker compose -f backend/deployments/docker-compose.yaml up --build

down:
	docker compose -f backend/deployments/docker-compose.yaml down

lint:
	golangci-lint run
