CC = gcc
CFLAGS = -Wall -Wextra -Isrc -Ilibs/include -O2
LDFLAGS = -Llibs/lib -lcwist -lcjson -lssl -lcrypto -lsqlite3 -luriparser -lpthread

SRCS = src/main.c src/db.c src/handlers.c src/utils.c
OBJS = $(SRCS:.c=.o)
TARGET = server

all: $(TARGET)

$(TARGET): $(OBJS)
	$(CC) $(OBJS) -o $(TARGET) $(LDFLAGS)

%.o: %.c
	$(CC) $(CFLAGS) -c $< -o $@

clean:
	rm -f $(OBJS) $(TARGET)

.PHONY: all clean
