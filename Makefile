.PHONY: build run clean install help

# Binary name
BINARY_NAME=dronebridge

# Build the application
build:
	@echo "Building $(BINARY_NAME)..."
	go build -o $(BINARY_NAME) .
	@echo "Build complete: $(BINARY_NAME)"

# Run the application
run: build
	@echo "Running $(BINARY_NAME)..."
	./$(BINARY_NAME)

# Install dependencies
install:
	@echo "Installing dependencies..."
	go mod download
	go mod tidy
	@echo "Dependencies installed"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f $(BINARY_NAME)
	@echo "Clean complete"

# Run with custom config
run-custom:
	@echo "Running with custom config..."
	./$(BINARY_NAME) -config $(CONFIG)

# Run in registration mode
run-register: build
	@echo "Running in registration mode..."
	./$(BINARY_NAME) --register

# Show help
help:
	@echo "Available targets:"
	@echo "  build        - Build the application"
	@echo "  run          - Build and run the application"
	@echo "  run-register - Build and run in registration mode (--register)"
	@echo "  install      - Install dependencies"
	@echo "  clean        - Remove build artifacts"
	@echo "  run-custom   - Run with custom config (usage: make run-custom CONFIG=path/to/config.yaml)"
	@echo "  help         - Show this help message"
