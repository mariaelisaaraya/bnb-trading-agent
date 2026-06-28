# Cómo correr el agente

Repositorio: **https://github.com/mariaelisaaraya/bnb-trading-agent**

---

## Requisitos

- Go 1.21 o superior
- Trust Wallet Agent Kit (TWAK) instalado: https://portal.trustwallet.com
- API key de CoinMarketCap Pro: https://coinmarketcap.com/api
- Una wallet en BSC con USDT y algo de BNB para gas

---

## Instalación

```bash
git clone https://github.com/mariaelisaaraya/bnb-trading-agent
cd bnb-trading-agent
go build -o bnb-agent .
```

---

## Configuración

El agente busca su configuración en `~/.bnb-trading-agent/`. Creá esa carpeta y los dos archivos:

**`~/.bnb-trading-agent/config.yaml`**
```yaml
cmc_api_key: "TU_API_KEY_DE_CMC"

twak:
  wallet_address: "0xTU_WALLET"
  password: "tu_password_de_twak"
  dry_run: false          # cambiar a true para probar sin trades reales

trade_interval_minutes: 15

strategy:
  fg_buy_threshold: 24    # comprar si F&G >= este valor
  fg_sell_threshold: 15   # vender todo si F&G <= este valor
  trend_buy_min_pct: -5.0
  trend_sell_max_pct: -8.0
  tokens:
    - symbol: ETH
      contract: "0x2170ed0880ac9a755fd29b2688956bd959f933f8"
      trade_amount_usd: 5
    - symbol: ATOM
      contract: "0x0eb3a705fc54725037cc9e008bdede697f62f335"
      trade_amount_usd: 4

policy:
  max_trade_usd: 10
  daily_loss_cap_usd: 8
  drawdown_cap: 0.25
  max_trades_per_hour: 10
```

**`~/.bnb-trading-agent/policy.yaml`**
```yaml
max_trade_usd: 200
daily_loss_cap_usd: 150
drawdown_cap: 0.90
max_trades_per_hour: 10
allowed_tokens:
  - ETH
  - ATOM
  - DOGE
  - USDT
  - BNB
```

---

## Correrlo

**Modo live (trades reales en BSC):**
```bash
./bnb-agent run --verbose
```

**Modo dry-run (sin trades reales, para probar):**
```bash
# Cambiá dry_run: true en config.yaml, luego:
./bnb-agent run --verbose
```

**Una sola evaluación (útil para cron):**
```bash
./bnb-agent run-once --verbose
```

**Ver el audit log:**
```bash
./bnb-agent audit
```

**Verificar la integridad del audit log (hash chain):**
```bash
./bnb-agent audit --verify
```

---

## Correrlo en background (producción)

```bash
nohup ./bnb-agent run --verbose >> /tmp/bnb-agent.log 2>&1 &
```

Para ver el log en tiempo real:
```bash
tail -f /tmp/bnb-agent.log
```

Para detenerlo:
```bash
kill $(pgrep -f "bnb-agent run")
```

---

## Registrar en la competencia BNB Hack (Track 1)

Antes de que abra la ventana de trading:
```bash
twak compete register
```

Esto registra tu wallet on-chain en el contrato de la competencia. Sin este paso, los trades no cuentan para el leaderboard.

---

## Tokens soportados

El agente funciona con cualquier token BEP-20 listado en CoinMarketCap. Para que los indicadores técnicos (EMA/RSI) estén disponibles, el token también necesita tener historia de precios en TWAK (`twak price <symbol> --history week`). Tokens confirmados con historia: ETH, ATOM, DOGE.

Para tokens sin historia, el agente usa solo Fear & Greed + momentum como señales.

---

## Estructura del proyecto

```
bnb-trading-agent/
├── agent.go        # loop principal y keep-alive
├── strategy.go     # lógica de trading (EMA, RSI, F&G)
├── indicators.go   # cálculo de EMA y RSI
├── guard.go        # pipeline de seguridad de 5 etapas
├── trader.go       # cliente de TWAK (compras y ventas)
├── market.go       # cliente de CoinMarketCap
├── audit.go        # audit log con SHA-256 hash chain
├── integrity.go    # registro y verificación de intents
├── state.go        # estado persistido en disco
├── policy.go       # lectura y validación de policy.yaml
├── config.go       # lectura de config.yaml
├── x402.go         # autofinanciamiento x402
└── main.go         # CLI (run, run-once, audit, demo)
```
