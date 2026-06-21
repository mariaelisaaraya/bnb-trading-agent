# BNB Trading Agent

**Autonomous trading agent for BNB Smart Chain with a deterministic 5-stage security pipeline.**

Built for [BNB Hack: AI Trading Agent Edition](https://dorahacks.io/hackathon/bnbhack-twt-cmc/detail) — CoinMarketCap × Trust Wallet × BNB Chain.

> Track 1 — Autonomous Trading Agents | Agent wallet: `0xD676C49F543D202A3422436F19C4B4d872a8Dd9f` | Registered on BSC ✓

---

## The problem with autonomous trading agents

Most trading agents have a critical blind spot: they trust their own decisions.

If a poisoned data source, compromised MCP server, or prompt injection attack corrupts the trade parameters between the strategy and the execution layer — the agent executes blindly:

```
Strategy decides:  "Sell BNB for $50 at $587"
                          ↓
              [Attacker tampers with params]
                          ↓
Agent executes:    "Buy RUGPULL token for $5,000"
```

There is no check. The money is gone.

---

## The solution: a guard pipeline between strategy and execution

This agent introduces a **deterministic 5-stage security pipeline** that intercepts every trade decision before TWAK executes it. No LLM in the guard — just regex, hashes, and arithmetic. You cannot prompt-inject a regex.

```
  CoinMarketCap AI Agent Hub
  (Fear & Greed + price quotes)
              ↓
      Fear & Greed Strategy
        BUY / SELL / HOLD
              ↓
  ┌───────────────────────────┐
  │   5-Stage Guard Pipeline  │  ← every trade intercepted here
  │                           │
  │  1. credential scan       │  private key in reason field? BLOCK
  │  2. policy check          │  trade size, daily cap, rate limit
  │  3. integrity check       │  token/direction tampered? BLOCK
  │  4. drawdown check        │  portfolio down 25%? STOP
  │  5. SHA-256 audit chain   │  tamper-evident log of every decision
  │                           │
  └───────────────────────────┘
              ↓ ALLOW only if all 5 pass
  Trust Wallet Agent Kit (TWAK)
    signs locally, executes swap
              ↓
         BSC Mainnet
       (PancakeSwap DEX)
```

---

## Sponsor technology integration

| Sponsor | Technology | How it's used |
|---------|-----------|---------------|
| **CoinMarketCap** | AI Agent Hub REST API | `/v3/fear-and-greed/latest` for market sentiment; `/v2/cryptocurrency/quotes/latest` for price, 24h/7d change, volume |
| **Trust Wallet** | Agent Kit CLI (TWAK) + x402 | Self-custody local signing — `twak swap`, `twak wallet balance`, `twak compete register`, `twak x402 pay`. Private keys never leave the machine. |
| **BNB Chain** | BSC Mainnet (chain ID 56) | All swaps and x402 payments execute on BSC. Agent registered on the competition contract. |

## x402 — autonomous self-funding

The agent uses [x402](https://github.com/x402-foundation/x402) to pay for services autonomously. No human top-up needed between sessions.

```
Agent balance: $2.40 (below $3.00 threshold)
      ↓
twak x402 request https://agentsvc.io/api/search \
  --max-payment 1000 --prefer-asset USDT \
  --prefer-network bsc --yes --auto-approve
      ↓
Server returns HTTP 402 → TWAK pays 0.001 USDT on BSC
      ↓
Service responds with data → agent uses it and continues
```

This is the x402 HTTP payment loop: server returns `402 Payment Required` with payment details → TWAK signs and pays from the local wallet → request retries with receipt → agent gets the data. Private keys never leave the machine.

`--max-payment` is in atomic units (USDT has 6 decimals: `$0.001 = 1000`).

Configure in `~/.bnb-trading-agent/config.yaml`:

```yaml
x402:
  enabled: true
  service_url: "https://agentsvc.io/api/search"
  payment_asset: "USDT"
  max_payment_usdc: 0.001     # $0.001 per request
  min_balance_usd: 3.0        # trigger when balance drops below $3
```

---

## Trading strategy

**Fear & Greed + Momentum** — driven entirely by CoinMarketCap data.

| Signal | Conditions | Logic |
|--------|-----------|-------|
| **BUY** | F&G ≥ 55 AND 24h ≥ +1% AND 7d ≥ 0 | Greed confirmed by short and medium trend |
| **SELL** | F&G ≤ 30 OR 24h ≤ -3% | Fear or sharp drop — exit position |
| **HOLD** | Everything else | No clear signal — stay flat |

The agent evaluates every 15 minutes. Non-trade decisions exit in under 1ms.

---

## The 5-stage security pipeline in detail

### Stage 1 — Credential scan
Scans every field of the trade decision (token, direction, amount, reason) for 10 credential patterns: EVM private keys, JWT tokens, API keys, AWS secrets, SSH keys, and more. If found → **BLOCK**.

### Stage 2 — Policy enforcement
Validates against configurable risk limits:
- Per-trade maximum ($200)
- Daily loss cap ($150)
- Rate limit (4 trades/hour)
- Token allowlist (BNB, USDT, CAKE, ETH, BTCB...)

### Stage 3 — Integrity check
Compares the current trade against the stored intent for this token. If the token or direction changed since the last registered intent → **BLOCK** (tampered).

### Stage 4 — Drawdown check
Tracks portfolio peak value. If current portfolio is 25% below peak → **STOP ALL TRADING**. Competition disqualifies at 30% — this gives a 5% safety buffer.

### Stage 5 — SHA-256 audit chain
Every decision (allow or block) is appended to a JSONL audit log with a SHA-256 hash of the previous entry. Tamper any line → the chain breaks. Verifiable with `bnb-agent audit --verify`.

---

## Demo — 7 attack scenarios

```
$ ./bnb-agent demo

BNB Trading Agent — Guard Pipeline Demo
═══════════════════════════════════════════════════════

Scenario 1: Normal buy — F&G=65, momentum positive
  Decision:  ✓ ALLOW
  Pipeline:  credentials:clean → policy:allow → integrity:registered → audit:logged

Scenario 2: Same buy repeated — integrity verified
  Decision:  ✓ ALLOW
  Pipeline:  credentials:clean → policy:allow → integrity:verified → audit:logged

Scenario 3: Amount inflation attack — $50 inflated to $5000
  Decision:  ✗ BLOCK
  Pipeline:  credentials:clean → policy:amount_exceeded
  Reason:    trade size $5000.00 exceeds per-trade limit $200.00

Scenario 4: Token swap attack — BNB swapped for unknown token
  Decision:  ✗ BLOCK
  Pipeline:  credentials:clean → policy:token_not_allowed
  Reason:    token RUGPULL is not on the allowed list

Scenario 5: Credential exfiltration — private key in reason field
  Decision:  ✗ BLOCK
  Pipeline:  credentials:DETECTED
  Reason:    credential detected in trade field "reason": EVM private key (0xaaa***)

Scenario 6: Rate limit flood — 4 trades already this hour
  Decision:  ✗ BLOCK
  Pipeline:  credentials:clean → policy:rate_limit
  Reason:    rate limit: 4 trades already executed this hour

Scenario 7: Daily loss cap reached
  Decision:  ✗ BLOCK
  Pipeline:  credentials:clean → policy:daily_loss_cap
  Reason:    daily loss cap $150.00 reached
```

---

## Live run output

```
$ ./bnb-agent run --verbose

BNB Trading Agent starting
  Token:    BNB
  Interval: 15m0s
  Mode:     LIVE (real trades on BSC)

[2026-06-20 22:28:52] Evaluating BNB...
  Price:     $587.85
  24h:       +1.10%
  7d:        -3.56%
  F&G:       21 (Extreme Fear)
  Signal:    sell — extreme fear: F&G=21 ≤ threshold 30
  Guard:     allow [credentials:clean → policy:allow → integrity:verified → audit:logged]
  Executed:  sell $50.00 BNB → USDT via PancakeSwap
  TxHash:    0x...
  Gas:       ~$0.05
```

---

## Risk controls

Configured in `~/.bnb-trading-agent/policy.yaml`:

```yaml
max_trade_usd: 200           # No single trade above $200
daily_loss_cap_usd: 150      # Stop after $150 net daily loss
drawdown_cap: 0.25           # Stop if portfolio drops 25% from peak
max_trades_per_hour: 4       # Rate limit — prevents runaway loops
slippage_tolerance: 0.02     # 2% max slippage enforced by TWAK --slippage flag
allowed_tokens:
  - BNB
  - USDT
  - CAKE
  - ETH
  - BTCB
```

The drawdown cap is deliberately set at 25% — 5 percentage points below the competition's 30% disqualification threshold.

---

## Audit trail

```
$ ./bnb-agent audit --verify

BNB Trading Agent — Audit Trail
══════════════════════════════════

  2026-06-20T22:18:37Z
  Trade:    sell BNB $50.00 @ $587.35
  Decision: allow
  Stages:   credentials:clean → policy:allow → integrity:verified → audit:logged
  Hash:     sha256:f4ea9d1e584a8...

  2026-06-20T22:33:51Z
  Trade:    hold BNB — F&G=28, no signal
  Decision: hold
  Hash:     sha256:a1c3b2d9f7e02...

Hash chain: VALID (2 entries, 0 breaks)
```

---

## Quick start

```bash
# 1. Clone and build
git clone https://github.com/mariaelisaaraya/bnb-trading-agent
cd bnb-trading-agent
go build -o bnb-agent .

# 2. Initialize config
./bnb-agent init
# Edit ~/.bnb-trading-agent/config.yaml:
# - cmc_api_key: your CoinMarketCap Pro API key
# - twak.wallet_address: your TWAK wallet
# - twak.password: your TWAK wallet password

# 3. Run the demo (no API keys needed)
./bnb-agent demo

# 4. Test with real CMC data, no trades
./bnb-agent run --dry-run --verbose --once

# 5. Register on BSC competition contract
./bnb-agent register

# 6. Go live
./bnb-agent run
```

---

## CLI reference

```
bnb-agent init       Initialize config directory and default files
bnb-agent run        Start the autonomous 15-minute trading loop
  --dry-run          Log decisions without executing trades
  --verbose          Print full market data and guard stages
  --once             Run one iteration and exit
bnb-agent register   Register agent wallet on BSC competition contract
bnb-agent audit      Print the tamper-evident trade audit trail
  --verify           Verify the SHA-256 hash chain
bnb-agent demo       Run 7 guard pipeline attack scenarios (no keys needed)
bnb-agent version    Print version
```

---

## Tech stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.26 — single binary, no runtime, no `node_modules` |
| Market data | CoinMarketCap AI Agent Hub (REST API) |
| Execution | Trust Wallet Agent Kit CLI (`twak`) |
| DEX | PancakeSwap V3 on BSC (via TWAK swap) |
| Chain | BNB Smart Chain mainnet (chain ID 56) |
| Security pipeline | Deterministic Go — regex, hashes, arithmetic |
| Audit log | JSONL with SHA-256 hash chain |
| State | YAML + `syscall.Flock` for concurrent-safe disk writes |
| Config | YAML policy file (hot-editable between iterations) |

---

## No smart contracts — pure agent architecture

Zero custom Solidity. Zero on-chain logic to audit or exploit.

- **Trading logic** — Go binary on your machine
- **Security pipeline** — in-process, before any network call
- **Swap execution** — TWAK → PancakeSwap's existing BSC contracts
- **Competition registration** — hackathon's pre-deployed contract via `twak compete register`

---

## Why Go, not Python or TypeScript

Three properties that matter specifically for an autonomous trading agent:

1. **Single binary** — `go build` produces one self-contained executable. No runtime, no package manager, no version conflicts. Works on any machine.

2. **Deterministic pipeline** — the security guard runs on every trade decision in under 5ms. An LLM-based guard adds seconds of latency and introduces prompt injection risk. Go's determinism means the guard behavior is provable and auditable.

3. **OS-level file locking** — the agent writes state between iterations (rate limits, spend records, portfolio peak). `syscall.Flock` gives atomic, concurrent-safe disk writes without a database.

---

## Why not just use an LLM to decide trades?

The security pipeline is deliberately **not** an LLM. Here's why:

| Approach | Risk |
|---------|------|
| LLM guard | Vulnerable to prompt injection in the reason field |
| Smart contract guard | Requires on-chain gas for every check; adds latency |
| Centralized API guard | Single point of failure; adds network dependency |
| **Deterministic Go guard** ✓ | You cannot prompt-inject a regex. Runs in <5ms. Fails closed. |

---

## Competition

- **Hackathon**: BNB Hack: AI Trading Agent Edition — CoinMarketCap × Trust Wallet × BNB Chain
- **Track**: Track 1 — Autonomous Trading Agents ($24,000 prize pool)
- **Trading window**: June 22–28, 2026 — BSC mainnet, live positions
- **Agent wallet**: `0xD676C49F543D202A3422436F19C4B4d872a8Dd9f`
- **Registration tx**: `0x5e8c883bd4e306e7462c5a18f1f5a1299ccc1a2e1b6e07d51303b7bb89a72805`
- **Starting capital**: ~$12 USDT + BNB for gas (self-funded, BSC mainnet)

---

## License

MIT
