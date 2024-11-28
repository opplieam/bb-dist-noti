# Distributed Notification System

![img](https://github.com/opplieam/bb-dist-noti/blob/main/bb-dist-noti.drawio.png?raw=true)

A high-performance, fault-tolerant distributed notification system designed to deliver real-time notifications 
with unparalleled speed and reliability. This system receives events from pipelines like NATS (Pub/Sub) 
and seamlessly notifies connected users through Server-Sent Events (SSE). Built for scalability, 
it also caches notifications in memory with an efficient eviction policy, ensuring rapid access to recent messages 
without querying external services.

## Key Features

- **Decentralized Service Discovery**  
  Leverages **Serf** with the gossip protocol for robust and decentralized node discovery.

- **State Consistency with Raft**  
  Implements **Raft consensus** to coordinate nodes, ensuring a single leader model and reliable replication across the cluster.

- **Efficient Log Management**  
  Uses Write-Ahead Logs (WAL) for both log and stable store, delivering superior performance.

- **Pipeline Connection**  
  Only the leader node establishes a secure subscription to the event stream pipeline (e.g., NATS JetStream).

- **Security First**  
  Supports **mutual TLS** for secure communication across all nodes.

- **Multiplexed RPC Connections**  
  Utilizes **gRPC** and **Raft RPC** for ease of use.

- **In-Memory Caching with Eviction**  
  Built-in append-only memory caching stores the most recent notifications (configurable, e.g., 500 or 1000 entries) 
  with a robust eviction policy.

- **Kubernetes Ready**  
  Comes with Helm charts for easy deployment on Kubernetes clusters.

## The Heart of the System: `agent.go`

At the core of this system lies the `agent` package. It orchestrates all critical components, including:

1. **Client Connection Management**  
   Maintains active connections with clients, handling reconnections gracefully during failures.

2. **HTTP and SSE Handling**  
   Manages HTTP requests and delivers real-time notifications to users via Server-Sent Events (SSE).

3. **gRPC and Membership Services**  
   Sets up a gRPC server for internal server-to-server communication. Leveraging Serf for node discovery and failure detection.

4. **Consensus and Coordination**  
   Ensures state consistency across nodes through the Raft algorithm, enabling leader election and state replication.

5. **Fault-Tolerant Shutdown**  
   Implements graceful shutdown procedures to protect the stability of the cluster and the WAL.

---

## Installation

### Prerequisites
- **Go 1.22+** 
- **Docker** and **Kubernetes**
- **minikube** (or equivalent local cluster solution)
- **Helm 3.x+**
- **cfssl** (for mTLS certificate generation)
- **protoc** (for protocol buffer compilation)

### Test

1. Generate certificate. Please check `tls/` for configuration
    ```bash
    make gencert    # Generates certificates in $HOME/.bb-noti/ 
    ```
2. Run Test
    ```bash
    make test       # Includes end-to-end testing via agent_test.go 
    ```

### Local deployment without cluster

1. Start Dependencies
   ```bash
    # Terminal 1: Start NATS JetStream
    make run-jet-stream

    # Terminal 2: Start mock publisher
    make run-mock-pub 
    ```
   
2. Launch Cluster Nodes
    ```bash
    # Terminal 3-5: Start three nodes
    make run-node-1    # Leader node (HTTP port 8402)
    make run-node-2    # Follower node (HTTP port 8502)
    make run-node-3    # Follower node (HTTP port 8602)
    ```

3. Verify Node Status
   ```bash
    curl http://localhost:8502/readiness    # Check Node 2
    curl http://localhost:8602/readiness    # Check Node 3
    ``` 
4. Connect to Service
   ```bash
    curl http://localhost:8502/category     # Connect via Node 2
    curl http://localhost:8602/category     # Connect via Node 3
    ```
5. Kill Node 1 or try to play around.

### Kubernetes deployment (Local cluster)

1. Build and Load Docker Image
    ```bash
    make docker-build-dev    # For minikube environments
    ```

2. Deploy Dependencies
    ```bash
   make run-jet-stream
   make run-mock-pub
    ```
3. Deploy Application
   ```bash
    make helm    # Deploys using configuration from deploy/bb-noti/values.yaml
    ```
4. Access Service by port forward (7000)

---
### Configuration

#### Application Configuration
Run `make help` to view available configuration flags

#### Key Configuration Files
- `deploy/bb-noti/values.yaml`: Kubernetes deployment configuration
- `tls/`: TLS certificate configuration

---

### Project structure

```bash
.
├── cmd/
│   ├── mockpub/          # Mock publisher implementation
│   └── noti/             # Main application entry point
├── deploy/               # Kubernetes and Helm configurations
├── internal/
│   └── agent/            # Core system implementation
│       └── agent_test.go # End-to-end tests
├── pkg/                  # Shared libraries
├── proto/                # Protocol buffer definitions
├── protogen/             # Generated protocol buffer code
└── tls/                  # TLS configuration files
```

---
## Future Improvements

- [ ] Implement more efficient data serialization
- [ ] Add support for real user ID authentication
- [ ] Enhance start-join-addrs configuration
- [ ] Enhance Serf cluster joining mechanism
- [ ] Implement load balancing for follower node connections
- [ ] Add comprehensive monitoring and metrics
- [ ] Improve cache eviction policies