.PHONY: build run clean install help

# Binary name
BINARY_NAME=dronebridge
BUILD_DIR=build

# Build the application
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) .
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Run the application
run: build
	@echo "Running $(BINARY_NAME)..."
	./$(BUILD_DIR)/$(BINARY_NAME)

# Install dependencies
install:
	@echo "Installing dependencies..."
	go mod download
	go mod tidy
	@echo "Dependencies installed"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf $(BUILD_DIR)
	@echo "Clean complete"

# Run with custom config
run-custom: build
	@echo "Running with custom config..."
	./$(BUILD_DIR)/$(BINARY_NAME) -config $(CONFIG)

# Run in registration mode
run-register: build
	@echo "Running in registration mode..."
	./$(BUILD_DIR)/$(BINARY_NAME) --register

# Show help
help:
	@echo "Available targets:"
	@echo "  build        - Build the application into build/"
	@echo "  run          - Build and run the application"
	@echo "  run-register - Build and run in registration mode (--register)"
	@echo "  install      - Install dependencies"
	@echo "  clean        - Remove build/ directory"
	@echo "  run-custom   - Run with custom config (usage: make run-custom CONFIG=path/to/config.yaml)"
	@echo "  help         - Show this help message"
