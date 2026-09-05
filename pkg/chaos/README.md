# package chaos

`pkg/chaos` provides fault-injection primitives for testing Local-WEB's
distributed protocols (DHT, CRDT, federation) under adverse network conditions.

## Components

### FaultyConn

A synthetic `net.Conn` for tests that need to simulate:

- **Packet loss** — `lossRate` (0.0 = perfect, 1.0 = all dropped)
- **Latency** — `WithLatency(conn, duration)`
- **Duplication** — `WithDuplicate(conn, n)` (each packet sent n extra times)
- **Partitioning** — `WithPartition(conn, true)` simulates network partition
  (all reads return 0)

```go
c := chaos.NewFaultyConn(100, 0.3)
chaos.WithLatency(c, 100*time.Millisecond)
// Use c as a net.Conn in tests
```

### NewPipe

Creates a pair of connected `net.Conn`s with configurable packet loss.
Data written to one end can be read from the other:

```go
a, b := chaos.NewPipe(1024, 0.1)
a.Write([]byte("hello"))
buf := make([]byte, 5)
n, _ := b.Read(buf) // reads "hello" with 10% chance of drop
```

## Usage in Tests

Chaos components implement `net.Conn` directly, so they can be used
anywhere a `net.Conn` or `io.Reader`/`io.Writer` is expected. They are
**opt-in only** — importing `pkg/chaos` does not affect production behavior
because nothing in production depends on it.

## Running

```bash
make test-chaos
```
