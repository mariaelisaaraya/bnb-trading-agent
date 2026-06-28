# Arquitectura del agente

## Diagrama de flujo

```
┌─────────────────────────────────────────────────────────────────┐
│                     BNB Trading Agent                           │
│                                                                 │
│   ┌──────────────┐      ┌──────────────────────────────────┐   │
│   │ CoinMarketCap│      │          ESTRATEGIA               │   │
│   │  Agent Hub   │─────▶│                                  │   │
│   │              │      │  • Fear & Greed Index (F&G)       │   │
│   │  • Precio    │      │  • EMA 7 períodos (corto plazo)   │   │
│   │  • 24h/7d %  │      │  • EMA 30 períodos (medio plazo)  │   │
│   │  • F&G Index │      │  • RSI 14 períodos               │   │
│   └──────────────┘      │  • CompositeScore ponderado       │   │
│                         │                                   │   │
│   ┌──────────────┐      │  BUY  si score ≥ 0.25            │   │
│   │  TWAK CLI    │      │  BUY  si RSI < 30 (oversold)     │   │
│   │  price hist  │─────▶│  SELL si F&G ≤ 15 (hard exit)   │   │
│   │  --history   │      │  SELL si 24h ≤ -8%  (stop-loss)  │   │
│   │  week --json │      │  HOLD en cualquier otro caso      │   │
│   └──────────────┘      └─────────────┬────────────────────┘   │
│                                       │ Señal: BUY / SELL / HOLD│
│                                       ▼                         │
│                    ┌──────────────────────────────────────┐     │
│                    │    PIPELINE DE SEGURIDAD (5 etapas)  │     │
│                    │                                      │     │
│                    │  1. CREDENTIAL SCAN                  │     │
│                    │     Bloquea si algún campo contiene  │     │
│                    │     clave privada, JWT, API key      │     │
│                    │            │                         │     │
│                    │            ▼                         │     │
│                    │  2. POLICY ENFORCEMENT               │     │
│                    │     Max trade USD, daily loss cap,   │     │
│                    │     rate limit, token allowlist      │     │
│                    │            │                         │     │
│                    │            ▼                         │     │
│                    │  3. INTEGRITY CHECK                  │     │
│                    │     Verifica que el token y dirección│     │
│                    │     no fueron modificados desde el   │     │
│                    │     intent registrado                │     │
│                    │            │                         │     │
│                    │            ▼                         │     │
│                    │  4. DRAWDOWN CHECK                   │     │
│                    │     Detiene todo si el portfolio     │     │
│                    │     cayó más del 25% desde el pico   │     │
│                    │            │                         │     │
│                    │            ▼                         │     │
│                    │  5. SHA-256 AUDIT LOG                │     │
│                    │     Registra la decisión con hash    │     │
│                    │     encadenado, verificable offline  │     │
│                    │            │                         │     │
│                    │     ALLOW ─┘  BLOCK ──▶ (detiene)   │     │
│                    └────────────┬─────────────────────────┘     │
│                                 │                               │
│                                 ▼                               │
│                    ┌────────────────────────┐                   │
│                    │   TRUST WALLET (TWAK)  │                   │
│                    │                        │                   │
│                    │  • Firma local         │                   │
│                    │  • Clave nunca sale    │                   │
│                    │    de la máquina       │                   │
│                    │  • twak swap USDT ETH  │                   │
│                    │    --slippage 5        │                   │
│                    │    --chain bsc         │                   │
│                    └────────────┬───────────┘                   │
│                                 │                               │
│                                 ▼                               │
│                    ┌────────────────────────┐                   │
│                    │   PancakeSwap V3       │                   │
│                    │   BSC Mainnet          │                   │
│                    │   Chain ID: 56         │                   │
│                    └────────────┬───────────┘                   │
│                                 │                               │
│                                 ▼                               │
│                    ┌────────────────────────┐                   │
│                    │   AUDIT LOG (JSONL)    │                   │
│                    │   SHA-256 hash chain   │                   │
│                    │   tamper-evident       │                   │
│                    │   verificable con:     │                   │
│                    │   bnb-agent audit      │                   │
│                    │              --verify  │                   │
│                    └────────────────────────┘                   │
└─────────────────────────────────────────────────────────────────┘
```

## El pipeline tomando una decisión real

A continuación, una decisión real del agente del 26 de junio de 2026 — venta de ATOM cuando el F&G cayó a 15 (protección por miedo extremo). Cada línea es un stage del pipeline.

```
[2026-06-26 00:47:34] Evaluating ATOM...
  Price:     $1.6157
  24h:       -2.10%
  7d:        -12.30%
  F&G:       15 (Extreme Fear)          ← disparador del hard exit
  EMA7/30:   $1.62 / $1.66 (↓ bear)
  RSI-14:    28.4
  Signal:    sell — extreme fear: F&G=15 <= threshold 15

  Guard:     allow [credentials:clean → policy:allow → integrity:registered]
  Executed:  sell $4.00 ATOM @ $1.6157
  TxHash:    [on-chain BSC]
```

Y el registro correspondiente en el audit log (real, tomado del archivo `audit.jsonl`):

```json
{
  "timestamp": "2026-06-26T00:47:34.209847Z",
  "trade_id": "trade_1782434854209340000",
  "action": "sell",
  "token": "ATOM",
  "direction": "sell",
  "amount_usd": 4,
  "price": 1.615662613191647,
  "decision": "allow",
  "pipeline_stages": [
    "credentials:clean",
    "policy:allow",
    "integrity:registered"
  ],
  "prev_hash": "sha256:8dc01271cbea4571c789c94fda79331533caff3c75c0bb8c6c75d53fc5823ef4",
  "hash": "sha256:0c533269b2aad54b899679030ed13da974c22f5c6777aec1000ba6083a2f302a"
}
```

Cada entrada encadena el hash de la anterior (`prev_hash`), formando una cadena que puede verificarse con:

```bash
./bnb-agent audit --verify
```
