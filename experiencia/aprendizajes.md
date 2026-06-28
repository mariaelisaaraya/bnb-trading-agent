# BNB Hack 2026 — Lo que aprendí

## Qué es BNB Chain y por qué es interesante

BNB Chain (antes Binance Smart Chain) es una blockchain compatible con Ethereum pero con bloques mucho más rápidos y gas mucho más barato. Esto la hace ideal para agentes autónomos que necesitan ejecutar muchas transacciones pequeñas sin que el costo de gas se coma las ganancias.

Lo que la hace especialmente interesante para proyectos de trading es el ecosistema: PancakeSwap es el DEX dominante, hay cientos de tokens listados en CoinMarketCap que ya tienen liquidez en BSC, y la infraestructura para agentes está mejorando rápido. En esta hackathon, BNB Chain, Trust Wallet y CoinMarketCap se juntaron justamente para construir esa infraestructura: TWAK para firmar transacciones sin ceder las claves, y el CMC Agent Hub para acceder a datos de mercado desde un agente.

La idea de que un agente pueda leer el mercado, decidir, y ejecutar on-chain sin intervención humana es algo que en 2026 ya es técnicamente posible — y este proyecto lo demostró en producción real.

---

## Por qué elegí Go

Elegí Go por tres razones concretas:

**1. Velocidad de ejecución.** El agente evalúa el mercado cada 15 minutos y tiene que tomar decisiones en milisegundos. En Go, el pipeline de seguridad de 5 etapas corre en menos de 5ms. Con Python o Node habría sido más lento y con más dependencias que pueden fallar.

**2. Un solo binario.** Con `go build` tengo un ejecutable que corre en cualquier máquina sin instalar runtime, sin entornos virtuales, sin `npm install`. Eso es importante cuando el agente tiene que correr semanas sin que nadie lo toque.

**3. Tipado estático.** Cuando estás manejando plata real, que el compilador te diga antes de correr que algo está mal es valioso. En Python, errores de tipo en producción con dinero real son muy costosos.

La desventaja fue que Go tiene menos librerías ready-to-use para DeFi que Python o TypeScript. Tuve que construir todo desde cero: el cliente de CMC, el parser de TWAK, los indicadores técnicos.

---

## Qué aprendí durante la hackathon

### Sobre mercados cripto
- El Fear & Greed Index es un indicador poderoso pero insuficiente solo. Toda la semana estuvo en 14-21 (extreme fear) y el mercado siguió cayendo. Necesitás combinar F&G con indicadores técnicos como EMA y RSI para tomar mejores decisiones.
- El RSI < 30 (oversold) puede ser una señal de compra contrarian, pero en mercados de tendencia bajista fuerte, "oversold can stay oversold". ATOM bajó a RSI=14 y siguió cayendo.
- Salir a tiempo importa más que entrar en el momento perfecto. La decisión de vender ATOM cuando F&G tocó 15 fue correcta — el mercado siguió cayendo después.

### Sobre infraestructura de agentes
- `twak price --history` se colgaba aleatoriamente y congelaba el proceso entero. Los agentes autónomos necesitan timeouts y watchdogs — si una llamada externa no responde, el agente tiene que seguir, no quedarse esperando para siempre.
- El problema APPROVAL_SENT_SWAP_FAILED es real: PancakeSwap genera una ruta de swap que tiene un deadline. Si el agente tarda en ejecutar (por cualquier motivo), la ruta expira, el approval ya fue, pero el swap nunca ocurre. La solución fue subir el slippage de 2% a 5%.
- El estado del agente (holdings, portfolio, intents) hay que guardarlo en disco y mantenerlo sincronizado con lo que realmente pasó on-chain. Hubo momentos donde el estado interno divergió de BscScan.

### Sobre participar en una hackathon de trading
- Registrar el agente on-chain el primer día es crítico. Perdí el día 1 de la ventana de trading porque el agente no estaba corriendo desde el arranque.
- Las reglas de "mínimo 1 trade por día" implican que necesitás un mecanismo de keep-alive robusto — uno que realmente ejecute on-chain, no solo que intente.
- El leaderboard no muestra nada hasta que empezás a cumplir todos los requisitos. Eso genera mucha incertidumbre al principio.

### Sobre seguridad en agentes autónomos
- Un pipeline de seguridad determinístico (sin LLM en el guard) es la decisión correcta. Si el guard tomara decisiones con un modelo de lenguaje, podría ser manipulado. Con regex y aritmética, no.
- La cadena de hashes SHA-256 para el audit log fue una de las mejores ideas del proyecto: podés probar que ninguna entrada fue modificada después de ser creada.
- La separación entre "intención" (registrar qué vas a hacer) y "ejecución" (hacer el swap) previene que un ataque de manipulación cambie el destino de los fondos entre las dos etapas.

---

## Qué mejoraría si lo hiciera de nuevo

### Técnico
- **Timeout en todas las llamadas externas.** Cada llamada a `twak price`, `twak balance`, o la API de CMC necesita un timeout configurable. Si no responde en X segundos, skip y seguir.
- **Keep-alive más robusto.** Reintentar el swap hasta 3 veces con backoff si el primero falla por route expiry. Actualmente si falla, el agente espera al próximo ciclo de 15 minutos.
- **Estado sincronizado con BscScan.** Cada N ciclos, reconciliar el estado interno contra los balances reales on-chain para detectar divergencias.
- **Más tokens con historia de precios.** Solo ETH, ATOM y DOGE tenían `twak price --history`. Tokens como CAKE, LINK, ZRO quedaban sin EMA/RSI y usaban solo F&G como señal.
- **Backtesting.** Implementar los indicadores primero y testear la estrategia contra datos históricos antes de correr con plata real.

### Estrategia
- **No comprar en tendencia bajista fuerte** aunque RSI esté oversold. Agregar un filtro de tendencia de más largo plazo (EMA de 90 días o 200 días).
- **Position sizing dinámico.** En vez de comprar siempre $4 o $5, ajustar el tamaño según la fuerza de la señal y la volatilidad del token.
- **Stop-loss por token** además del drawdown global del portfolio.

### Operativo
- **Arrancar el agente el día 0** antes de que abra la ventana de trading, no el día 1.
- **Monitoring externo.** Una alerta por Telegram o email si el agente no escribe al log en más de 30 minutos.
- **Script de watchdog.** Un proceso separado que detecte si el agente está congelado y lo reinicie automáticamente.

---

## Por qué este proyecto vale la pena más allá de la hackathon

La mayoría de los agentes de trading cripto que existen hoy son de dos tipos: bots simples que siguen reglas fijas sin seguridad, o interfaces sobre LLMs que no tienen garantías determinísticas. Este proyecto prueba un tercer camino: un agente autónomo con inteligencia real (indicadores técnicos, señales de mercado) pero con un escudo de seguridad que no puede ser manipulado por inyección de prompts ni por datos corruptos.

Eso tiene valor en el mundo real. Alguien que quiera dejar correr un agente de trading sin mirarlo por semanas necesita exactamente eso: saber que si los datos de entrada están comprometidos, el agente no va a ejecutar un trade dañino.

El pipeline de 5 etapas, el audit log con hash chain, y la arquitectura de autofinanciamiento con x402 son piezas que podrían escalar a un producto real.
