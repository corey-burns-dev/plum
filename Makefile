# Variables
DOCKER_COMPOSE ?= docker compose
BUN ?= bun

# Colors
BLUE := \033[1;34m
GREEN := \033[1;32m
YELLOW := \033[1;33m
RED := \033[1;31m
NC := \033[0m # No Color

.PHONY: help dev build up down logs logs-app logs-frontend ps restart clean lint fmt test

# Default target
help:
	@echo "$(BLUE)╔════════════════════════════════════════════════════════════════╗$(NC)"
	@echo "$(BLUE)║             Plum - Full Stack Development CLI                ║$(NC)"
	@echo "$(BLUE)╚════════════════════════════════════════════════════════════════╝$(NC)"
	@echo ""
	@echo "$(GREEN)Development:$(NC)"
	@echo "  make dev         - 🚀 Start full stack in development mode"
	@echo "  make build       - 🔨 Build all Docker images"
	@echo "  make up          - ⬆️  Start services in background"
	@echo "  make down        - ⬇️  Stop all services"
	@echo "  make restart     - 🔄 Restart all services"
	@echo ""
	@echo "$(GREEN)Logs:$(NC)"
	@echo "  make logs        - 📋 Stream all logs"
	@echo "  make logs-app    - 📋 Backend logs only"
	@echo "  make logs-frontend - 📋 Frontend logs only"
	@echo ""
	@echo "$(GREEN)Code Quality:$(NC)"
	@echo "  make lint        - 🔍 Lint both backend and frontend"
	@echo "  make fmt         - 🎨 Format both backend and frontend"
	@echo ""
	@echo "$(GREEN)Testing:$(NC)"
	@echo "  make test        - 🧪 Run backend tests"
	@echo ""
	@echo "$(GREEN)Cleanup:$(NC)"
	@echo "  make clean       - 🧹 Remove containers, volumes, and temp files"

dev:
	$(DOCKER_COMPOSE) up

build:
	$(DOCKER_COMPOSE) build

up:
	$(DOCKER_COMPOSE) up -d

down:
	$(DOCKER_COMPOSE) down

restart:
	$(DOCKER_COMPOSE) restart

logs:
	$(DOCKER_COMPOSE) logs -f

logs-app:
	$(DOCKER_COMPOSE) logs -f app

logs-frontend:
	$(DOCKER_COMPOSE) logs -f frontend

ps:
	$(DOCKER_COMPOSE) ps

lint: lint-backend lint-frontend

lint-backend:
	cd backend && golangci-lint run

lint-frontend:
	cd frontend && $(BUN) x oxlint

fmt: fmt-backend fmt-frontend

fmt-backend:
	cd backend && go fmt ./...

fmt-frontend:
	cd frontend && $(BUN) x oxfmt

test:
	cd backend && go test -v ./...

clean:
	$(DOCKER_COMPOSE) down -v
	rm -rf backend/tmp backend/bin frontend/dist
