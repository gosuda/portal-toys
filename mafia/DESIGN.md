# Mafia Backend Design

## Domain Priming
- **Problem Class**: Real-time, stateful social deduction game server supporting multiple independent rooms over Portal relay WebSockets.
- **Inputs**: WebSocket commands (`join`, `chat`, `action`, `vote`, `admin`) carrying player identifiers and payloads.
- **Outputs**: Broadcast events (text feeds, system prompts, timer ticks, state changes) delivered to all room members; HTTP responses for health checks.
- **Constraints**: Go 1.25.3, zero shared state between rooms, deterministic timers (day/night/vote), need to mirror `mafia/reference.js` role logic, deployable over Portal SDK + optional local HTTP.
- **Success Metrics**: ≤100 ms intra-room event latency at 50 concurrent rooms, timers fire within ±200 ms, no race conditions (`-race` clean), job abilities produce identical outcomes to reference, rooms garbage-collected within 5 s of last disconnect.
- **Unknowns**: Front-end rendering expectations, exact WebSocket payload schema, maximum concurrent connections per room.
- **Relevant APIs**: RFC 6455 WebSocket, Portal SDK (`sdk.NewClient`, `Listen`), Zerolog logging, Cobra CLI.

## Architecture Δ
```
+------------+        +------------------+        +---------------------+
| Browser/   |  WS    | Portal Relay     |  RD    | Portal SDK Client   |
| Game UI    +------->+ (bootstrap URLs) +------->+ (credential, listen) |
+------------+        +------------------+        +----------+----------+
                                                        |
                                                        v
                                              +---------------------+
                                              | HTTP mux (/ws,/hc)  |
                                              +----------+----------+
                                                         |
                                                         v
                                              +---------------------+
                                              | WebSocket Handler   |
                                              | - upgrade           |
                                              | - authenticate      |
                                              +----------+----------+
                                                         |
                                                         v
                                              +---------------------+
                                              | RoomManager (Hub)   |
                                              | - map[channel]*Room |
                                              | - player registry   |
                                              +----------+----------+
                                                         |
                              +--------------------------+-------------------------+
                              |                          |                         |
                        +-----v-----+              +-----v-----+             +-----v-----+
                        | Room A    |              | Room B    |             | Room ...  |
                        | - state   |              | - state   |             |           |
                        | - timers  |              | - timers  |             |           |
                        +-----+-----+              +-----+-----+             +-----+-----+
                              |                          |                         |
                 +------------+------------+             |                         |
                 | Job Engine & Dispatcher |<------------+-------------------------+
                 +-------------------------+
```

## Data Flow Δ
```
Client JSON -> WS Handler -> decode Command -> RoomManager.Route(room)
      -> Room.Enqueue(command) -> state machine updates (chat/day/night/vote)
      -> build Event payloads -> broadcast via each Client.send channel -> WebSocket write
```

## Concurrency Δ
```
main goroutine
  ├─ Portal relay http.Serve (goroutine per listener)
  ├─ Optional local http.Server
  ├─ Signal watcher (ctx)
RoomManager
  ├─ mutex protects room map/player index
  └─ each Room owns goroutine: event loop + timers
Room
  ├─ stateMu guards state mutation
  ├─ broadcast loops iterate over snapshot of clients
  └─ timers (time.Timer) post commands into room channel
Client connection
  ├─ reader goroutine -> decode -> send to room channel
  └─ writer goroutine -> listens on send chan -> websocket.WriteJSON
Lock hierarchy: RoomManager.mu → Room.stateMu → Client send (never reverse) to avoid deadlock.
```

## Memory Layout Δ
```
Room struct:
  name string
  stateMu sync.Mutex
  players map[string]*Client
  order []string
  jobs map[string]*JobSpec
  playerJobs map[string]*AssignedJob
  prefix map[string]map[string]string
  selectMap map[string]map[string]string
  trick map[string]string
  timers struct { phase *time.Timer; vote *time.Timer }
  history []Event
Client struct:
  conn *websocket.Conn (owned by handler)
  send chan Event (bounded buffer)
  user string
  room *Room
All rooms referenced from RoomManager map; stale rooms removed when players=0.
```

## Optimization Δ
- **Event Fan-out**: snapshot client list before broadcast to minimize lock hold; large rooms use buffered send channel to avoid writer blocking.
- **Timer Wheel**: day/night/vote timers reuse single goroutine per room; reusing `time.Timer` reduces allocations.
- **Prefix Cache**: memoize `prefix[sender][target]` lookups to avoid recomputing role labels.
- **Command Routing**: map `command` strings to handler funcs to avoid reflection.
- **Memory Control**: cap history slices (e.g., last 200 events) with ring buffer semantics to prevent unbounded growth per room.
```
Bounded send channel (32) -> drop oldest on overflow to protect server.
``` 
