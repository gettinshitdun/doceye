BINARY_NAME=doceye
INSTALL_PATH=/usr/local/bin

.PHONY: build install uninstall clean

build:
	@echo "Building $(BINARY_NAME)..."
	@go build -o $(BINARY_NAME) .
	@echo "[OK] Build complete"

install: build
	@echo "Installing $(BINARY_NAME) to $(INSTALL_PATH)..."
	@sudo cp -f $(BINARY_NAME) $(INSTALL_PATH)/$(BINARY_NAME)
	@sudo chmod +x $(INSTALL_PATH)/$(BINARY_NAME)
	@echo "[OK] Installed to $(INSTALL_PATH)/$(BINARY_NAME)"

uninstall:
	@echo "Removing $(BINARY_NAME) from $(INSTALL_PATH)..."
	@sudo rm -f $(INSTALL_PATH)/$(BINARY_NAME)
	@echo "[OK] Uninstalled"

clean:
	@echo "Cleaning..."
	@rm -f $(BINARY_NAME)
	@echo "[OK] Clean complete"
