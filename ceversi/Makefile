CC = gcc
CFLAGS = -Wall -Wextra -O3
LDFLAGS = -lcwist -lttak -lcjson -lssl -lcrypto -luriparser -lpthread -ldl -lm

SRCS = src/main.c src/db.c src/handlers.c src/utils.c src/memory.c src/betting_logic.c
OBJS = $(SRCS:.c=.o)
TARGET = server
WASM_SRC = src/betting_logic_wasm.c
WASM_OUT = public/betting_logic.wasm

all: $(TARGET) wasm

$(TARGET): $(OBJS)
	$(CC) $(OBJS) -o $(TARGET) $(LDFLAGS)

%.o: %.c
	$(CC) $(CFLAGS) -c $< -o $@

clean:
	rm -f $(OBJS) $(TARGET) $(WASM_OUT)

wasm: $(WASM_OUT)

$(WASM_OUT): $(WASM_SRC) src/betting_logic.c src/betting_logic.h
	clang --target=wasm32 -O3 -nostdlib \
	-Wl,--no-entry -Wl,--export=wasm_betting_single_delta \
	-Wl,--export=wasm_betting_can_wager -Wl,--export=wasm_betting_project_points \
	-Wl,--export=wasm_betting_multiplayer_reward \
	$(WASM_SRC) src/betting_logic.c -o $(WASM_OUT)

.PHONY: all clean wasm
