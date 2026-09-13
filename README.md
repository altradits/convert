# Yebente - M-Pesa to Bitcoin Lightning Settlement Gateway

Yebente is a production-grade infrastructure platform that enables automated fiat-to-Bitcoin Lightning onramping by bridging **Safaricom M-Pesa STK push payments** with real-time **Bitcoin Lightning Network** settlements.

---

## 1. System Architecture

```
                                      +------------------------------------+
                                      |   Flutter Mobile App / Web Gateway |
                                      +-----------------+------------------+
                                                        |
                                    REST API (HTTPS/JSON)|
                                                        v
+---------------------------------------------------------------------------------------------------+
| Yebente Backend Gateway (Go 1.22+ / Gin)                                                         |
|                                                                                                   |
|  +---------------------+   +-----------------------+   +---------------------+   +-------------+  |
|  |     Auth Handler    |   |    Payment Handler    |   |    M-Pesa Handler   |   | Rate Engine |  |
|  |   (OTP / Sessions)  |   | (Deposit / Instant)   |   | (Idempotent Webhook)|   | (CoinGecko) |  |
|  +----------+----------+   +-----------+-----------+   +----------+----------+   +------+------+  |
|             |                          |                          |                     |         |
|             v                          v                          v                     v         |
|  +---------------------------------------------------------------------------------------------+  |
|  |                        Settlement Engine & Multi-Lightning Router                           |  |
|  |   - BOLT11 Decoder (`lnbc...`)   - LNURL-pay Resolver (`lnurl1...` / Lightning Address)     |  |
|  +--------------------+-----------------------------------+--------------------+---------------+  |
|                       |                                   |                    |                  |
+-----------------------|-----------------------------------|--------------------|------------------+
                        |                                   |                    |
       +----------------+                +------------------+                    +------------------+
       | (Database Pool)                 | (HTTP / REST API)|                    | (gRPC / TLS)     |
       v                                 v                  v                    v                  v
+-----------------------------+   +--------------+   +---------------+   +--------------------------+
| PostgreSQL 15+              |   | Blink API    |   | LNbits Wallet |   | LND Node (REST / gRPC)   |
| - Users & OTP Verification  |   | (Galoy Sv)   |   | (Admin Key)   |   | (Macaroon & TLS Cert)    |
| - Transactions & State Logs |   +--------------+   +---------------+   +--------------------------+
+-----------------------------+
```

---

## 2. Key Capabilities

- ⚡ **Multi-Lightning Provider Routing**: Route payments dynamically via **Blink (Galoy)**, **LNbits**, or a native **LND node** (REST or gRPC).
- 🔍 **Universal Lightning Destination Support**: Automatically validates and resolves:
  - **BOLT11 Invoices** (`lnbc...`, `lntb...`, `lnbcrt...`)
  - **LNURL-pay Links** (`lnurl1...`, `lnurlp://...`, `https://.../.well-known/lnurlp/...`)
  - **Lightning Addresses** (`user@domain.com`)
- 📱 **Safaricom Daraja STK Push**: Automates mobile checkout prompts directly to Kenyan phone numbers (+254).
- 🛡️ **Idempotent Webhook Processing**: Safely handles Safaricom callbacks with database transaction locking.
- 💱 **Live Exchange Rate Quoting**: Real-time KES to Satoshi conversions via CoinGecko / Kraken price indices.
- 💻 **Cross-Platform Access**: Includes both a responsive **Web Gateway Dashboard** and a native **Flutter Mobile App**.

---

## 3. Directory Layout

```
.
├── backend/
│   ├── cmd/
│   │   └── api/
│   │       └── main.go                         # API entrypoint & router configuration
│   ├── internal/
│   │   ├── config/
│   │   │   └── config.go                       # Environment and secret loader
│   │   ├── domain/
│   │   │   ├── payment.go                      # Payment domain models & validations
│   │   │   ├── payment_test.go                 # Domain unit tests
│   │   │   └── user.go                         # User domain model
│   │   ├── handler/
│   │   │   ├── auth_handler.go                 # OTP generation & verification
│   │   │   ├── mpesa_handler.go                # Daraja STK push callback webhook
│   │   │   └── payment_handler.go              # Payment deposit, instant settle, invoice decoding
│   │   ├── repository/
│   │   │   └── postgres/
│   │   │       ├── db.go                       # PostgreSQL connection pool
│   │   │       ├── payment_repo.go             # Idempotent transaction state persistence
│   │   │       └── user_repo.go                # User onboarding & session persistence
│   │   ├── service/
│   │   │   ├── bolt11_decoder.go               # Pure Go BOLT11, bech32 & LNURL decoder
│   │   │   ├── bolt11_decoder_test.go          # Decoder test suite
│   │   │   ├── daraja_service.go               # Safaricom Daraja STK Push client
│   │   │   ├── lightning_provider.go           # Multi-provider router (Blink, LNbits, LND)
│   │   │   ├── rate_service.go                 # Live BTC/KES exchange rate engine
│   │   │   ├── settlement_service.go           # Core orchestrator (Fiat in -> Lightning out)
│   │   │   └── settlement_service_test.go      # Settlement engine test suite
│   │   └── secret/
│   │       └── gcp_secrets.go                  # GCP Secret Manager integration
│   ├── migrations/
│   │   ├── 000001_init_schema.up.sql           # Database schema migration
│   │   └── 000001_init_schema.down.sql
│   ├── web/
│   │   └── index.html                          # Web Gateway Dashboard UI
│   ├── .env.example                            # Configuration environment template
│   ├── Dockerfile                              # Multi-stage production container
│   ├── go.mod
│   └── go.sum
├── frontend/                                   # Flutter Mobile App
│   ├── lib/
│   │   ├── core/
│   │   │   ├── constants/api_constants.dart
│   │   │   └── utils/lightning_validator.dart
│   │   ├── features/
│   │   │   ├── auth/presentation/phone_input_screen.dart
│   │   │   └── deposit/presentation/
│   │   │       ├── deposit_screen.dart
│   │   │       └── tx_status_screen.dart
│   │   └── main.dart
│   └── pubspec.yaml
└── index.html                                  # Root Web Gateway UI
```

---

## 4. Developer Setup Guide

Follow these step-by-step instructions to set up, configure, and run the complete Yebente system locally.

### Step 1: Prerequisites

Make sure you have the following installed on your machine:
- **Go**: Version 1.22 or higher (`go version`)
- **PostgreSQL**: Version 14 or higher (`psql --version`)
- **Flutter SDK**: Version 3.0+ (Optional, for the mobile client: `flutter --version`)
- **Git** & **cURL**

---

### Step 2: Database Initialization

1. Start your local PostgreSQL server:
   ```bash
   # macOS (Homebrew)
   brew services start postgresql@15

   # Ubuntu / Debian
   sudo systemctl start postgresql
   ```

2. Create the Yebente database:
   ```bash
   createdb yebente_db
   ```

3. Run the schema migrations:
   ```bash
   psql -d yebente_db -f backend/migrations/000001_init_schema.up.sql
   ```

---

### Step 3: Configure Environment Variables

1. Copy the sample environment file:
   ```bash
   cd backend
   cp .env.example .env
   ```

2. Edit `.env` with your settings:
   ```env
   # Server Port & Mode
   PORT=8080
   ENVIRONMENT=development

   # PostgreSQL Database URL
   DATABASE_URL=postgres://postgres:password@localhost:5432/yebente_db?sslmode=disable

   # ---------------------------------------------------------------------------
   # Lightning Funding Provider (Choose at least ONE)
   # ---------------------------------------------------------------------------
   
   # OPTION A: Blink (Galoy) API Key (Fastest Setup)
   BLINK_API_KEY=your_blink_api_key_here

   # OPTION B: LNbits Wallet
   # LNBITS_URL=https://legend.lnbits.com
   # LNBITS_ADMIN_KEY=your_lnbits_admin_key_here

   # OPTION C: LND Node (REST API)
   # LND_REST_URL=https://node.example.com:8080
   # LND_MACAROON_HEX=0201036c6e...

   # OPTION D: Native LND Node (gRPC)
   # LND_GRPC_ADDRESS=localhost:10009
   # LND_MACAROON_PATH=/path/to/admin.macaroon
   # LND_TLS_CERT_PATH=/path/to/tls.cert

   # ---------------------------------------------------------------------------
   # Safaricom Daraja M-Pesa Credentials
   # ---------------------------------------------------------------------------
   DARAJA_BASE_URL=https://api.safaricom.co.ke
   DARAJA_CONSUMER_KEY=your_daraja_consumer_key
   DARAJA_CONSUMER_SECRET=your_daraja_consumer_secret
   DARAJA_PASSKEY=your_daraja_passkey
   DARAJA_SHORTCODE=your_paybill_or_till_number
   DARAJA_CALLBACK_URL=https://your-public-domain.com/api/v1/mpesa/callback
   ```

---

### Step 4: Connecting a Lightning Provider

To execute real Lightning transactions, configure at least one of the following providers:

#### Option A: Blink / Galoy (Recommended for Rapid Testing)
1. Download the [Blink Wallet](https://www.blink.sv/) on your phone or visit [dashboard.blink.sv](https://dashboard.blink.sv).
2. Go to **Settings > Developer > API Keys** and generate an API key with `Read` and `Write` permissions.
3. Add `BLINK_API_KEY=your_key` in `.env` (or enter it via the Web UI Wallet Modal).

#### Option B: LNbits Instance
1. Access your self-hosted LNbits instance or an LNbits host.
2. Create or open a wallet.
3. Click **API info** and copy the **Admin Key** (`X-Api-Key`).
4. Add `LNBITS_URL` and `LNBITS_ADMIN_KEY` to `.env`.

#### Option C: LND Node (REST or gRPC)
1. For REST access, obtain your node's URL and export your invoice/admin macaroon in hex format:
   ```bash
   xxd -p -c 2000 ~/.lnd/data/chain/bitcoin/mainnet/admin.macaroon
   ```
2. For gRPC access, ensure port `10009` is reachable and provide `LND_GRPC_ADDRESS`, `LND_MACAROON_PATH`, and `LND_TLS_CERT_PATH`.

---

### Step 5: Setting up Safaricom Daraja (M-Pesa)

1. **Create a Developer Account**:
   - For Sandbox: Register at [developer.safaricom.co.ke](https://developer.safaricom.co.ke).
   - For Production: Register your Paybill/Till on the Safaricom Daraja Production Portal.
2. **Create an App**:
   - Create a new App and enable **Lipa na M-Pesa Sandbox / Production**.
   - Copy your `Consumer Key` and `Consumer Secret`.
3. **Webhook Callback URL (Local Development)**:
   - Safaricom Daraja requires a public HTTPS URL to deliver STK Push status callbacks.
   - For local development, expose your port 8080 using `ngrok` or `cloudflared`:
     ```bash
     ngrok http 8080
     ```
   - Copy the HTTPS forwarding URL (e.g., `https://abc123.ngrok-free.app`) and set:
     ```env
     DARAJA_CALLBACK_URL=https://abc123.ngrok-free.app/api/v1/mpesa/callback
     ```

---

### Step 6: Running the Backend Gateway

1. Build and run the Go backend:
   ```bash
   cd backend
   go run ./cmd/api
   ```

2. Alternatively, compile and run the optimized binary:
   ```bash
   cd backend
   go build -o bin/yebente-api ./cmd/api
   ./bin/yebente-api
   ```

3. Verify the server is running:
   ```bash
   curl http://localhost:8080/healthz
   ```
   **Expected Response:**
   ```json
   {
     "status": "UP",
     "environment": "development",
     "lightning_provider": "Blink",
     "timestamp": "2026-09-14T01:50:00Z"
   }
   ```

4. Open the Web Gateway UI in your browser:
   ```
   http://localhost:8080
   ```

---

### Step 7: Running the Flutter Mobile App (Optional)

1. Navigate to the frontend directory:
   ```bash
   cd frontend
   ```

2. Install Flutter dependencies:
   ```bash
   flutter pub get
   ```

3. Launch the app (iOS Simulator, Android Emulator, or Web):
   ```bash
   # For Chrome
   flutter run -d chrome

   # For iOS Simulator
   flutter run -d iOS

   # For Android Emulator (Uses http://10.0.2.2:8080 by default)
   flutter run -d android
   ```

---

## 5. API Reference

### Health & Configuration

#### `GET /healthz`
Returns system health, environment mode, and active Lightning provider.

#### `GET /api/v1/lightning/config`
Returns the currently connected Lightning provider, wallet status, and balance in satoshis.

#### `POST /api/v1/lightning/config`
Dynamically connects a funding Lightning provider at runtime without restarting the server.
```json
{
  "provider_type": "blink",
  "api_key": "your_api_key_here"
}
```

---

### Rates & Invoice Decoding

#### `GET /api/v1/rates`
Fetches live exchange rates for BTC/KES and Sats/KES.

#### `POST /api/v1/lightning/decode-invoice`
Decodes BOLT11 invoice parameters and calculates the estimated KES equivalent.
```json
{
  "invoice": "lnbc10u1pj..."
}
```

#### `POST /api/v1/lightning/resolve-address`
Resolves LNURL-pay endpoints or Lightning Addresses (`name@domain.com`) to generate a live BOLT11 invoice.
```json
{
  "address": "user@blink.sv",
  "amount_sats": 500
}
```

---

### Payments & Settlements

#### `POST /api/v1/payments/deposit`
Initiates an M-Pesa STK Push prompt to the user's phone for settlement to the specified Lightning invoice.
```json
{
  "phone_number": "+254712345678",
  "amount_kes": 100.0,
  "invoice": "lnbc10u1pj..."
}
```

#### `GET /api/v1/payments/:id/status`
Polls the live status of a payment transaction.
- Possible statuses: `PENDING_STK`, `MPESA_CONFIRMED`, `LIGHTNING_PENDING`, `SETTLED`, `MPESA_FAILED`, `SETTLEMENT_FAILED`.

#### `POST /api/v1/payments/instant-settle`
Executes an immediate off-chain Lightning payment directly through the active provider node.
```json
{
  "invoice": "lnbc10u1pj...",
  "amount_kes": 50.0,
  "phone": "+254712345678"
}
```

#### `POST /api/v1/mpesa/callback`
Webhook endpoint receiving Safaricom Daraja STK Push callbacks. Automatically triggers Lightning dispatch upon M-Pesa confirmation.

---

## 6. Testing

### Run All Unit Tests
```bash
cd backend
go test -v ./...
```

### Test Individual Packages
```bash
# Test BOLT11 & LNURL decoding
go test -v ./internal/service -run TestDecode

# Test Settlement Service & Multi-Engine Routing
go test -v ./internal/service -run TestSettlement

# Test Domain Validations
go test -v ./internal/domain -run TestTransaction
```

---

## 7. Production Deployment Guide (Google Cloud)

### A. Docker Container Build
Build and push the multi-stage Docker image:
```bash
docker build -t gcr.io/yebente-production/api:latest -f backend/Dockerfile backend/
docker push gcr.io/yebente-production/api:latest
```

### B. Cloud SQL (PostgreSQL)
1. Provision a Cloud SQL PostgreSQL 15+ instance.
2. Apply the schema migration:
   ```bash
   psql "postgres://yebente_user:<PASSWORD>@<CLOUD_SQL_IP>/yebente_prod" -f backend/migrations/000001_init_schema.up.sql
   ```

### C. Secret Manager
Store sensitive credentials in GCP Secret Manager:
```bash
echo -n "your_daraja_consumer_key" | gcloud secrets create DARAJA_CONSUMER_KEY --data-file=-
echo -n "your_daraja_consumer_secret" | gcloud secrets create DARAJA_CONSUMER_SECRET --data-file=-
echo -n "your_daraja_passkey" | gcloud secrets create DARAJA_PASSKEY --data-file=-
echo -n "your_blink_api_key" | gcloud secrets create BLINK_API_KEY --data-file=-
```

### D. Cloud Run Deployment
Deploy the container with VPC egress for private Cloud SQL and LND node connectivity:
```bash
gcloud run deploy yebente-api \
    --image=gcr.io/yebente-production/api:latest \
    --region=europe-west1 \
    --vpc-connector=yebente-connector \
    --vpc-egress=private-ranges-only \
    --service-account=yebente-backend-sa@yebente-production.iam.gserviceaccount.com \
    --set-env-vars=ENVIRONMENT=production,DARAJA_BASE_URL=https://api.safaricom.co.ke,DARAJA_SHORTCODE=your_shortcode \
    --allow-unauthenticated
```

---

## 8. License

MIT License. Built for open financial access and Bitcoin Lightning onramping.
