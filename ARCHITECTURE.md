# Arquitetura

Este documento explica as decisões de design por trás do entregável do
desafio wager-service, o porquê delas terem sido tomadas e — de forma
explícita, conforme solicitado — todos os pontos em que esta implementação
diverge das sugestões básicas do desafio.

## 1. Substituições solicitadas (leia isto primeiro)

A base do desafio sugere `net/http`, AWS SQS/LocalStack, e deixa a
autenticação/autorização em aberto. Por instrução explícita do
solicitante, este entregável substitui:

| Sugestão da base | Usado no lugar | Por quê |
|---|---|---|
| `net/http` | [chi](https://github.com/go-chi/chi) | Solicitado explicitamente. |
| AWS SQS / LocalStack | **RabbitMQ** | Pois tenho mais experiência operando RabbitMQ. Veja a §6 para entender o que isso muda. |
| Autenticação não especificada | **Keycloak** (OAuth2/OIDC) | Solicitado explicitamente; a especificação permite qualquer mecanismo apropriado. Veja a §7. |
| `google/uuid` | pacote `uuid` nativo da stdlib do Go 1.27 | Solicitado explicitamente. Veja a §9. |

## 2. Camadas (Layering)

```
internal/domain        Go puro, zero imports de framework/infra, testado exaustivamente com testes unitários
internal/ports          interfaces das quais a camada de aplicação depende
internal/application    casos de uso: orquestram domínio + ports, testados com unit tests contra fakes
internal/infra/*         adaptadores concretos: postgres, rabbitmq, keycloak, http, config
internal/infra/fxmodules  wiring do Uber Fx: constrói cada adaptador, gerencia o ciclo de vida
cmd/api                  ponto de entrada do processo: fx.New(fxmodules.Module()).Run()
```

Cada camada interna não sabe nada sobre as camadas ao seu redor:
`domain` não importa nada específico do projeto, `application` importa
apenas `domain` e `ports`, e cada pacote de infra implementa uma
interface de `ports` sem que a camada de aplicação jamais importe pgx,
amqp091-go, chi ou fx diretamente. É isso que torna a lógica de
concorrência e idempotência da camada de aplicação testável com testes
unitários contra fakes simples em memória, em vez de um banco de dados
real (`internal/application/fakes_test.go`).

## 3. Dinheiro (Money)

`internal/domain/money.Money` armazena um valor como `int64` em unidades
mínimas (centavos) mais um código de moeda ISO — nunca um float, e nunca
`decimal.Decimal` com arredondamento implícito. `Parse` rejeita qualquer
coisa que não seja exatamente `-?[0-9]+\.[0-9]{2}`: sem notação
científica, sem `NaN`/`Inf`, sem escala decimal incorreta, sem string
vazia. Todas as operações aritméticas (`Add`/`Subtract`/`Negate`) têm
verificação de overflow e de moeda. `ParseExternalAmount` também rejeita
valores negativos, já que nenhuma operação submetida por um provedor pode
ter valor negativo (um REFUND é um valor positivo que credita a
carteira, não um débito negativo).

## 4. Concorrência: sem locks globais

Dois invariantes precisavam valer simultaneamente: nenhuma atualização
perdida (lost update) em uma única carteira sob submissões concorrentes,
e carteiras independentes sendo processadas totalmente em paralelo (sem
um lock de granularidade grossa em todo o serviço).

- **A serialização por carteira** é obtida com `SELECT ... FOR UPDATE`
  (`WalletRepository.FindByIDForUpdate`) dentro de uma transação de
  banco de dados — toda escrita em uma carteira adquire primeiro o lock
  daquela linha, de forma que dois escritores concorrentes visando a
  *mesma* carteira serializam sobre o próprio lock de linha do Postgres,
  enquanto escritores visando carteiras *diferentes* nunca disputam
  recursos entre si.
- **Concorrência otimista** (`Wallet.version`, verificada pelo
  `WHERE id = $1 AND version = $2` de `WalletRepository.Save`) é a
  segunda camada, independente: mesmo que uma futura refatoração venha a
  ler uma carteira fora de uma transação com `FOR UPDATE`, uma escrita
  desatualizada ainda seria rejeitada com `ErrOptimisticLock` em vez de
  sobrescrever silenciosamente uma mais recente.
- Dois cenários obrigatórios são comprovados sob o detector de race do
  Go em `internal/application/process_wager_transaction_test.go`:
  `TestConcurrency_SameBetSubmitted50TimesInParallel_SingleDebit` (a
  mesma BET submetida 50 vezes concorrentemente produz exatamente um
  débito, via idempotência — veja a §5) e
  `TestConcurrency_TwoCompetingBetsExceedBalance_OneProcessedOneRejected`
  (duas BETs distintas que juntas excedem o saldo: exatamente uma
  PROCESSED, uma REJECTED, o saldo final e o ledger refletem apenas a
  que foi processada).
- `TestConcurrency_DifferentWalletsProcessIndependently` comprova o
  inverso: carteiras não relacionadas nunca se bloqueiam mutuamente.

## 5. Idempotência

Duas camadas independentes, deliberadamente não misturadas:

1. **Idempotência de negócio** — `(providerId, idempotencyKey)` é o
   contrato voltado ao provedor. A primeira requisição sob uma
   determinada chave cria a transação; toda requisição subsequente sob a
   *mesma* chave é comparada por um hash SHA-256 canônico do seu
   payload de negócio (`application.CanonicalHash`, calculado sobre
   provider, id externo, jogador, carteira, round, jogo, tipo,
   valor+moeda e referência — veja `canonicalPayloadFrom`). Um payload
   idêntico reproduz o resultado original
   (`ProcessResult.IdempotentReplay = true`); um payload *diferente* sob
   a mesma chave resulta em `ErrIdempotencyKeyReused`, um erro definitivo
   de cliente. `(providerId, externalTransactionId)` é verificado de
   forma cruzada e independente, de modo que o mesmo id externo nunca
   possa ser reenviado sob uma chave de idempotência diferente
   (`ErrExternalTransactionKeyMismatch`).
2. **Deduplicação de entrega do broker (a inbox)** — indexada por
   `(consumerName, messageId)`, existe puramente para colapsar
   reentregas at-least-once da *mesma mensagem do broker*,
   independentemente de qual operação de negócio ela carrega. É
   populada apenas para o ponto de entrada do RabbitMQ
   (`ProcessWagerTransactionCommand.Inbox`); requisições HTTP não
   possuem messageId e dependem apenas da idempotência de negócio.

Ambas as camadas são duráveis (tabelas do Postgres, não caches em
memória) e ambas sobrevivem a um reinício de processo, já que a
verificação ocorre contra dados já confirmados (committed) na requisição
seguinte, e não contra estado em memória populado desde o boot.

## 6. Substituição do RabbitMQ: diferenças comportamentais exatas em relação ao SQS

- **Ordenação.** O SQS FIFO com um `MessageGroupId` por carteira garante
  entrega ordenada dentro de um grupo. O RabbitMQ oferece entrega
  at-least-once e, com uma única fila, entrega em ordem para aquela
  fila, mas não oferece nativamente uma partição de ordenação por chave.
  **Isso não afeta a corretude aqui**: a serialização em nível de
  carteira é garantida no banco de dados (§4), então duas operações
  contra a mesma carteira são seguras para serem processadas
  concorrentemente ou fora de ordem no broker — o banco de dados, e não
  o broker, é a fonte da verdade sobre "o que aconteceu primeiro".
- **Deduplicação.** O `MessageDeduplicationId` do SQS FIFO é substituído
  pela inbox (§5), indexada por `(consumerName, messageId)`. Toda
  mensagem publicada por este serviço carrega um `MessageId` estável
  (`rabbitmq.EventPublisher.Publish` o define como o próprio UUID do
  evento).
- **Dead-lettering / redrive.** A política de redrive gerenciada do SQS
  é reproduzida com uma dead-letter exchange do RabbitMQ mais uma fila
  de retry com TTL por mensagem para backoff exponencial (veja
  `internal/infra/rabbitmq/topology.go` para a topologia exata de
  exchanges/filas, e os métodos `retry`/`deadLetter` em `consumer.go`).
  Uma mensagem que esgota `Config.MaxRetries` cai em
  `wager.transactions.dlq` para inspeção manual e redrive, em vez de uma
  DLQ do SQS.
- **Confirmações do publisher** (`EventPublisher.Publish` aguarda o ack
  do broker via `PublishWithDeferredConfirmWithContext`) fecham a lacuna
  de durabilidade entre "a escrita TCP teve sucesso" e "o RabbitMQ
  persistiu isso de forma durável", que é o que o worker de outbox
  depende antes de marcar um registro como publicado.

## 7. Keycloak: autenticação e isolamento de provedores

Os provedores se autenticam como clientes OAuth2 via o grant
client-credentials. A claim `azp` (authorized party) do JWT — o client
ID ao qual o Keycloak emitiu o token — é tratada como o `providerId`
autenticado (`internal/infra/keycloak/middleware.go`,
`Config.ProviderClaim`, padrão `"azp"`).

**A identidade do provedor nunca é fornecida pelo cliente.** Nenhum
handler, e nenhum campo de `ProcessWagerTransactionCommand` derivado do
corpo de uma requisição, jamais obtém `providerId` do JSON enviado pelo
chamador. Os handlers HTTP o obtêm exclusivamente de
`keycloak.ProviderIDFromContext`, populado pelo middleware de
autenticação após verificar a assinatura do token (contra chaves obtidas
do endpoint JWKS do realm), o emissor (issuer) e, opcionalmente, a
audiência. É isso que torna estruturalmente impossível uma requisição
submeter uma operação, ou ler uma transação, como um provedor diferente
daquele cujas credenciais apresentou — veja a verificação explícita
`pathProviderID != authenticatedProviderID` em
`Handlers.GetWagerTransactionByExternalID`, e a verificação equivalente
em `Handlers.GetWagerTransaction` contra o próprio `ProviderID()` da
transação carregada.

As chaves JWKS são obtidas de forma preguiçosa (lazy) e armazenadas em
cache, sendo atualizadas apenas em caso de cache miss (um `kid` não
reconhecido, o sinal normal de rotação de chaves do próprio Keycloak) e
limitadas a uma vez a cada 30 segundos, de forma que uma enxurrada de
tokens carregando um `kid` falso não possa ser usada para sobrecarregar
o endpoint JWKS do Keycloak.

## 8. Outbox / inbox transacional

Toda alteração de domínio que também precisa emitir um evento de
integração (`WagerTransactionProcessed`, `WagerTransactionRejected`,
`WalletBalanceChanged`, `WagerTransactionPendingReference`) anexa o
envelope do evento à tabela `outbox_records` na *mesma exata* transação
de banco de dados que a alteração em si
(`ports.OutboxRepository.Append`, chamado de dentro de
`TxManager.WithinTx`). Um worker separado
(`internal/infra/rabbitmq/outbox_worker.go`) faz polling, reivindica
(`SELECT ... FOR UPDATE SKIP LOCKED`, seguro para múltiplas instâncias
de worker), publica e marca como publicado ou reagenda com backoff em
caso de falha. Isso garante que um evento nunca é publicado para uma
alteração que foi revertida (rolled back), e que uma alteração nunca é
confirmada (committed) sem que seu evento eventualmente seja publicado
(at-least-once, deduplicado no consumidor pelo próprio `EventId`
estável do evento).

A inbox é a imagem espelhada no lado do consumo (§5.2).

## 9. Versão do Go e o pacote `uuid` da stdlib

O `go.mod` declara `go 1.27.1`, a mesma versão usada pelo `Dockerfile`
(`golang:1.27.1-alpine`), e o código usa diretamente o pacote `uuid` da
standard library do Go 1.27 (`UUID`, `New`, `NewV4`, `NewV7`, `Parse`,
`MustParse`, `Nil`, `Max`, `Compare`, marshal/unmarshal de texto) — sem
nenhuma dependência de terceiros como `google/uuid`.

## 10. Uma regra personalizada não explícita na especificação: uma reversão por BET

A especificação permite que tanto REFUND quanto ROLLBACK revertam uma
BET, mas não diz o que acontece se *ambos* forem submetidos contra a
mesma BET. Ambos produzem o efeito financeiro idêntico — devolver a
aposta — então permitir que ambos tenham sucesso pagaria o jogador em
dobro. A regra desta implementação, aplicada em
`ProcessWagerTransactionUseCase.processReversal` e em
`ResolvePendingReferencesUseCase.attempt` verificando
`CountSuccessfulReversals` para **ambos** os tipos antes de confirmar
qualquer um deles:

> Uma BET referenciada pode receber no máximo uma reversão bem-sucedida
> no total — um REFUND *ou* um ROLLBACK, nunca os dois, e nunca um
> segundo do mesmo tipo.

Uma segunda tentativa de reversão (de qualquer tipo) contra uma BET já
revertida é rejeitada com `FailureDuplicateReversal`, e não silenciosamente
ignorada, de forma que o provedor sempre receba uma resposta definitiva
e auditável.

## 11. Cobertura de testes

Medida com `go test -race -coverprofile=...`:

| Pacote | Cobertura |
|---|---|
| `internal/domain/money` | 93.5% |
| `internal/domain/ledger` | 96.8% |
| `internal/domain/wager` | 98.9% |
| `internal/domain/wallet` | 91.8% |
| `internal/domain/events` | 92.3% |
| `internal/application` | 84.9% |
| **Combinada (`internal/domain/...` + `internal/application/...`)** | **89.4%** |

`internal/domain/domainerr` está em 0%/no-op: são erros sentinela e
definições de tipos pequenos, sem lógica de ramificação para cobrir.

Tudo sob `internal/infra/*` e `internal/infra/fxmodules` não possui
testes unitários, já que se trata exclusivamente de adaptadores finos
sobre pgx, amqp091-go, chi e a API HTTP do Keycloak.

## 12. Códigos de falha

Toda rejeição de `WagerTransaction` carrega um `FailureCode` estável e
documentado (`internal/domain/domainerr`):

| Código | Significado |
|---|---|
| `INSUFFICIENT_FUNDS` | Uma BET não pôde ser coberta pelo saldo atual. |
| `REVERSAL_EXCEEDS_BALANCE` | Um REFUND/ROLLBACK debitaria mais do que a carteira atualmente possui. |
| `REFERENCE_NOT_FOUND` | A transação externa referenciada nunca foi resolvida antes que o orçamento de retentativas para uma referência pendente se esgotasse. |
| `REFERENCE_MISMATCH` | A transação referenciada existe, mas diverge em provider/player/wallet/currency/round/amount. |
| `REFERENCE_NOT_PROCESSED` | A transação referenciada existe, mas nunca chegou ao estado PROCESSED. |
| `DUPLICATE_REVERSAL` | A transação referenciada já possui uma reversão bem-sucedida (§10). |
| `INVALID_TRANSITION` | Uma transição de estado foi tentada a partir de um estado terminal. |
