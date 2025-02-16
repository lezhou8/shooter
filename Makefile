CLIENT_DIR=./cmd/client
SERVER_DIR=./cmd/server
BUILD_DIR=./build
CLIENT_BIN=$(BUILD_DIR)/client
SERVER_BIN=$(BUILD_DIR)/server

$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

$(CLIENT_BIN): $(BUILD_DIR)
	go build -o $(CLIENT_BIN) $(CLIENT_DIR)

$(SERVER_BIN): $(BUILD_DIR)
	go build -o $(SERVER_BIN) $(SERVER_DIR)

.PHONY: client
client: $(CLIENT_BIN)

.PHONY: server
server: $(SERVER_BIN)

.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)
