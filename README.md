# Yebente - M-Pesa to Bitcoin Lightning Settlement Gateway

Yebente is a production-grade infrastructure platform that enables automated fiat-to-Bitcoin Lightning onramping by bridging Safaricom M-Pesa STK push payments with an isolated LND (Lightning Network Daemon) node.

---

## 1. System Architecture

```
                                  +-----------------------+
                                  |   Flutter Mobile App  |
                                  +-----------+-----------+
                                              |
                          REST API (HTTPS)    |
                                              v
+---------------------------------------------------------------------------------+
| Google Cloud Run (Yebente Backend API)                                         |
|                                                                                 |
|  +---------------------+   +-----------------------+   +---------------------+  |
|  |     Auth Handler    |   |    Payment Handler    |   |    M-Pesa Handler   |  |
|  |   (OTP Onboarding)  |   |   (Deposit / Status)  |   | (Idempotent Webhook)|  |
|  +----------+----------+   +-----------+-----------+   +----------+----------+  |
|             |                          |                          |             |
|             v                          v                          v             |
|  +---------------------------------------------------------------------------+  |
|  |                            Settlement Engine                              |  |
|  +--------------------+-----------------------------------+------------------+  |
|                       |                                   |                     |
+-----------------------|-----------------------------------|---------------------+
                        | (Private VPC)                     | (Private VPC)
                        v                                   v
+-----------------------------------+   +---------------------------------------+
|  Cloud SQL (PostgreSQL 15+)       |   |  Compute Engine / Private LND Node    |
|  - Users & Verification           |   |  - gRPC Port 10009                    |
|  - Transactions & Idempotency Lock|   |  - Custom Off-Chain Macaroon & TLS   |
+-----------------------------------+   +---------------------------------------+
```

---

## 2. Directory Layout

```
.
├── backend/
│   ├── cmd/
│   │   └── api/
│   │       └── main.go
│   ├── internal/
│   │   ├── config/
│   │   │   └── config.go
│   │   ├── domain/
│   │   │   ├── payment.go
│   │   │   ├── payment_test.go
│   │   │   └── user.go
│   │   ├── handler/
│   │   │   ├── auth_handler.go
│   │   │   ├── mpesa_handler.go
│   │   │   └── payment_handler.go
│   │   ├── repository/
│   │   │   └── postgres/
│   │   │       ├── db.go
│   │   │       ├── payment_repo.go
│   │   │       └── user_repo.go
│   │   ├── service/
│   │   │   ├── daraja_service.go
│   │   │   ├── lnd_service.go
│   │   │   ├── settlement_service.go
│   │   │   └── settlement_service_test.go
│   │   └── secret/
│   │       └── gcp_secrets.go
│   ├── migrations/
│   │   ├── 000001_init_schema.up.sql
│   │   └── 000001_init_schema.down.sql
│   ├── .env.example
│   ├── Dockerfile
│   ├── go.mod
│   └── go.sum
└── frontend/
    ├── lib/
    │   ├── core/
    │   │   ├── constants/
    │   │   │   └── api_constants.dart
    │   │   └── utils/
    │   │       └── lightning_validator.dart
    │   ├── features/
    │   │   ├── auth/
    │   │   │   └── presentation/
    │   │   │       └── phone_input_screen.dart
    │   │   └── deposit/
    │   │       └── presentation/
    │   │           ├── deposit_screen.dart
    │   │           └── tx_status_screen.dart
    │   └── main.dart
    └── pubspec.yaml
```

---

## 3. Backend Setup & Run

### Prerequisites
- Go 1.22+
- PostgreSQL 15+

### Database Migration
Run the SQL migration script on your PostgreSQL instance:
```bash
psql $DATABASE_URL -f backend/migrations/000001_init_schema.up.sql
```

### Running Locally
```bash
cd backend
cp .env.example .env
# Fill in your database and credentials
go run ./cmd/api
```

### Running Tests
```bash
cd backend
go test -v ./...
```

---

## 4. Frontend Setup & Run

### Prerequisites
- Flutter SDK 3.0+

```bash
cd frontend
flutter pub get
flutter run
```

---

## 5. Google Cloud Production Deployment

### A. LND Compute VM
1. Launch Debian 12 Compute Engine instance attached to `yebente-vpc` without external IP.
2. Bake restricted off-chain macaroon:
   ```bash
   lncli bakemacaroon offchain:read offchain:write --save_to=yebente_offchain.macaroon
   xxd -p -c 2000 yebente_offchain.macaroon
   ```
3. Restrict port `10009` access to backend subnet `10.0.1.0/24`.

### B. Serverless VPC Access Connector
```bash
gcloud compute networks vpc-access connectors create yebente-connector \
    --region=europe-west1 \
    --range=10.8.0.0/28 \
    --network=yebente-vpc
```

### C. Cloud Run Deployment
```bash
gcloud run deploy yebente-api \
    --image=gcr.io/yebente-production/api:latest \
    --region=europe-west1 \
    --vpc-connector=yebente-connector \
    --vpc-egress=private-ranges-only \
    --service-account=yebente-backend-sa@yebente-production.iam.gserviceaccount.com \
    --set-env-vars=GCP_PROJECT_ID=yebente-production,LND_GRPC_ADDRESS=10.0.2.15:10009 \
    --allow-unauthenticated
```
