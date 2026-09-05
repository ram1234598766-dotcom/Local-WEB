# Chaos Engineering for LocalWEB

This package provides fault injection primitives for chaos engineering and resilience testing.

## Components

- **FaultyConn** - Wraps connections with packet loss, latency, duplication, partition
- **Pipe** - In-memory lossy pipe for testing
- **ChaosRunner** - Orchestrates chaos experiments with scenarios
- **Scenario** - Defines a chaos experiment (partition, latency, loss, etc.)

## Usage

```go
// Create a lossy pipe
conn1, conn2 := chaos.NewPipe(4096, 0.1) // 10% packet loss

// Run a chaos scenario
runner := chaos.NewRunner()
runner.AddScenario(chaos.Scenario{
    Name:        "network-partition",
    Duration:    30 * time.Second,
    LossRate:    0.5,
    Latency:     100 * time.Millisecond,
    Partition:   true,
    TargetPeers: []string{"*"}, // all peers
})
runner.Run(ctx)
```

## CI Integration

Add to `.github/workflows/chaos.yml`:
```yaml
name: Chaos Tests
on:
  schedule:
    - cron: '0 2 * * *'  # Nightly
  push:
    branches: [main]
jobs:
  chaos:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: go test -race -v ./pkg/chaos/...
      - run: go test -race -v -run ChaosIntegration ./test/chaos/...
```

## Built-in Scenarios

- `partition` - Network partition (drop all packets)
- `high-latency` - Add 100-500ms latency
- `packet-loss` - 10-50% packet loss
- `duplicate` - Duplicate packets
- `corruption` - Corrupt packet data
- `mixed` - Combination of above