# Diagram gallery

Diagrams 1-4 describe the implemented fake-model runtime. Diagram 5 is explicitly
the planned rolling-deployment extension. The [roadmap](roadmap.md) and
[evidence ledger](project-tour.md#evidence-ledger) separate demonstrated behavior
from future design.

## 1. System context

The client-facing Go gateway owns API concerns. The controller owns placement,
routing, and recovery decisions. Python workers own model execution and batching.
The existing five-node Raft cluster stores durable control-plane state.

```mermaid
flowchart LR
    C["Client"] -->|"HTTP or gRPC"| G["Go API gateway"]
    G -->|"request ID and deadline"| S["Go scheduler/controller"]
    S <-->|"registration and lifecycle writes"| K["Five-node Raft KV"]

    subgraph P["Inference data plane"]
        W1["Python worker A"]
        W2["Python worker B"]
        W3["Python worker C"]
    end

    S -->|"load-aware gRPC routing"| W1
    S -->|"load-aware gRPC routing"| W2
    S -->|"load-aware gRPC routing"| W3
    W1 -.->|"heartbeat and load"| S
    W2 -.->|"heartbeat and load"| S
    W3 -.->|"heartbeat and load"| S

    G --> M["Prometheus"]
    S --> M
    W1 --> M
    W2 --> M
    W3 --> M
    M --> D["Grafana"]
```

## 2. Durable state versus live routing state

The most important control-plane decision is what deserves a consensus write.
Recoverable intent uses Raft; fast-changing observations remain in memory and
must be refreshed after a controller restart.

```mermaid
flowchart TB
    A["Control-plane input"] --> Q{"Does correctness require recovery or one global order?"}
    Q -->|"Yes"| R["Raft-backed durable state"]
    Q -->|"No"| E["Ephemeral controller state"]

    R --> R1["Worker ID and generation"]
    R --> R2["Declared model capabilities"]
    R --> R3["Drain and unavailable transitions"]
    R -.-> RP["Planned: deployment intent and routing config"]

    E --> E1["Last heartbeat time"]
    E --> E2["Queue depth"]
    E --> E3["Active request count"]
    E --> E4["Recent latency samples"]

    R --> C["Registration and lifecycle admission"]
    E --> S["Load-aware scheduling"]
    C --> S
```

## 3. Normal request lifecycle

The end-to-end deadline and request ID cross every service boundary. Batching is
worker-local; the controller does not need model-runtime knowledge.

```mermaid
sequenceDiagram
    autonumber
    participant Client
    participant Gateway
    participant Controller
    participant Worker

    Client->>Gateway: Generate(prompt, model, deadline)
    Gateway->>Controller: Route(request ID, model, deadline)
    Controller->>Controller: Filter by version, readiness, and lease
    Controller->>Controller: Rank by queue and active load
    Controller->>Worker: Generate(request ID, attempt 1)
    Worker->>Worker: Enqueue request
    Worker->>Worker: Form batch by size or queue deadline
    Worker-->>Controller: Text and token counts
    Controller-->>Gateway: Worker response
    Gateway-->>Client: Generate response
```

## 4. Worker failure and bounded recovery

There are two distinct failure paths. New requests stop routing to a worker when
its lease expires. An in-flight request is retried only when its deadline and the
documented retry semantics allow another attempt.

```mermaid
flowchart TD
    H["Worker heartbeat becomes late"] --> S["Mark worker suspect"]
    S --> A{"Heartbeat before lease expiry?"}
    A -->|"Yes"| R["Return worker to ready set"]
    A -->|"No"| U["Mark worker unavailable"]
    U --> N["Exclude worker from new routing"]
    U --> I{"In-flight request affected?"}
    I -->|"No"| W["Wait for newer worker generation"]
    I -->|"Yes"| B{"Retry budget, deadline, and semantics permit?"}
    B -->|"Yes"| T["Retry same request ID with next attempt"]
    B -->|"No"| F["Return explicit failure or unknown outcome"]
    T --> X["Choose another ready worker"]
```

## 5. Planned rolling model deployment

The controller reconciles durable desired state rather than mutating every worker
at once. A new version receives traffic only after readiness; old workers drain
admitted work before removal.

```mermaid
flowchart LR
    D["Commit desired version v2 to Raft"] --> C["Controller observes new generation"]
    C --> S["Start v2 workers"]
    S --> H{"v2 workers ready and healthy?"}
    H -->|"No"| P["Pause or roll back"]
    H -->|"Yes"| T["Shift new requests to v2"]
    T --> R["Mark v1 workers draining"]
    R --> Q{"Admitted v1 work complete?"}
    Q -->|"No"| R
    Q -->|"Yes"| X["Remove v1 workers"]
```

## 6. Milestone progression

Each stage adds one new distributed-systems concern while preserving a runnable
vertical slice.

```mermaid
flowchart LR
    M0["0: Contracts"] --> M1["1: Single worker"]
    M1 --> M2["2: Lifecycle and routing"]
    M2 --> M3["3: Dynamic batching"]
    M3 --> M4["4: Failure recovery"]
    M4 --> M5["5: Metrics and load"]
    M5 --> M6["6: Rolling deployments"]
```
