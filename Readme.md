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

- **Dynamic Load Balancing with Ingress Nginx**  
  Uses Ingress Nginx to load balance traffic only to follower nodes.
  Includes a custom Kubernetes operator for node labeling to ensure proper traffic routing.

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

### Kubernetes Deployment (Local Cluster)

#### 0. Install Nginx Ingress Controller
Nginx Ingress Controller is required for HTTP protocol routing.

```bash
make install-nginx
```

#### 1. Build and Load Docker Image
Build and load the application image into your local cluster. Choose the appropriate command based on your environment:

```bash
make docker-build-dev-minikube    # For Minikube environment
make docker-build-dev-kind        # For Kind environment
```

#### 2. Deploy Dependencies
Ensure that required dependencies are running before deploying the application:

```bash
make run-jet-stream   # Starts NATS JetStream
make run-mock-pub     # Starts mock publisher for testing
```

#### 3. Deploy Application
Deploy the main application using Helm. The configuration is defined in `deploy/bb-noti/values.yaml`:

```bash
make helm
```

#### 4. Deploy Custom Kubernetes Operators
For dynamic node labeling, deploy the custom Kubernetes operator:
[bb-dist-noti-operator](https://github.com/opplieam/bb-dist-noti-operator)

#### 5. Expose Application
For Kind: Port-forward Nginx to expose the application.
```bash
kubectl port-forward svc/nginx-ingress-controller -n ingress-nginx 8080:80
```
For Minikube: Use the Minikube tunnel.

```bash
minikube tunnel
```

Also, update /etc/hosts with:
```
127.0.0.1 bb-noti.localhost
```
Access the application at http://bb-noti.localhost.

---
## Configuration

### Application Configuration
Run `make help` to view available configuration flags

### Key Configuration Files
- `deploy/bb-noti/values.yaml`: Kubernetes deployment configuration
- `tls/`: TLS certificate configuration

---

## Project structure

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
## Note about Kubernetes Operator

While there may be simpler and more efficient solutions than using a Kubernetes operator, I chose to explore 
the operator model specifically to learn how to build one using Kubebuilder.

---
## Future Improvements

- [x] Add golangci-lint
- [ ] Upgrade gRPC to Opaque
- [x] Implement more efficient data serialization
- [ ] Add support for real user ID authentication
- [x] Enhance start-join-addrs configuration
- [x] Enhance Serf cluster joining mechanism
- [x] Implement load balancing for follower node connections
- [ ] Add comprehensive monitoring and metrics
- [x] Improve cache eviction policies
- [] Handle full outage. 