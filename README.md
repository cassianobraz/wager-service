# wager-service

Serviço distribuído de processamento de apostas para o desafio Backend
take-home (_"Desafio Backend — Processamento Distribuído de Apostas em
Go"_): um serviço em Go + Uber Fx que processa operações de BET, WIN,
LOSS, REFUND e ROLLBACK para provedores de jogos via HTTP e RabbitMQ, com
precisão monetária estrita, idempotência, controle de concorrência por
carteira e um ledger auditável e somente de inserção (append-only).

**Leia primeiro o [ARCHITECTURE.md](ARCHITECTURE.md)** — ele documenta
todas as substituições explícitas feitas em relação à base do desafio
(chi em vez de `net/http`, RabbitMQ em vez de SQS, Keycloak para
autenticação, o pacote `uuid` da stdlib do Go 1.27 em vez de
`google/uuid`), o raciocínio por trás do design de concorrência e
idempotência, e as limitações conhecidas deste entregável (sem Docker
ao vivo no sandbox de desenvolvimento; veja ARCHITECTURE.md §11).

## Pré-requisitos

- Docker e Docker Compose (para a stack completa: Postgres, RabbitMQ,
  Keycloak e a API).
- Go 1.27.1, apenas se quiser compilar/executar fora do Docker (o
  `go.mod` declara `go 1.27.1`; veja ARCHITECTURE.md §9).

## Executando a stack completa

```sh
docker compose up --build
```

Isso inicia, em ordem: Postgres, um container `migrate` de execução
única que aplica todos os arquivos em `migrations/` (via
[golang-migrate](https://github.com/golang-migrate/migrate)), RabbitMQ
(UI de gerenciamento em http://localhost:15672, guest/guest), Keycloak
(http://localhost:8081, admin/admin, realm `wager` importado
automaticamente a partir de
`deployments/keycloak/realm-export.json` com dois clientes provedores
de teste, `provider-a`/`provider-b`), e por fim a própria API em
http://localhost:8080.

Para executar apenas a infraestrutura e a API separadamente (por
exemplo, para iterar na API com `go run`):

```sh
docker compose up postgres migrate rabbitmq keycloak
cp .env.example .env
export $(grep -v '^#' .env | xargs)
go run ./cmd/api
```

## Autenticando como um provedor

`provider-a` e `provider-b` são ambos clientes OAuth2 pré-provisionados
usando o grant client-credentials. Obtenha um token:

```sh
curl -s -X POST \
  http://localhost:8081/realms/wager/protocol/openid-connect/token \
  -d grant_type=client_credentials \
  -d client_id=provider-a \
  -d client_secret=provider-a-secret \
  | jq -r .access_token
```

A claim `azp` do token (`provider-a`) é o que a API trata como o
`providerId` autenticado — nunca um valor obtido do corpo da requisição
(veja ARCHITECTURE.md §7).

## Passo a passo da API

```sh
TOKEN=$(curl -s -X POST http://localhost:8081/realms/wager/protocol/openid-connect/token \
  -d grant_type=client_credentials -d client_id=provider-a -d client_secret=provider-a-secret \
  | jq -r .access_token)

# 1. Abrir uma carteira com um saldo inicial (cria uma transação OPENING + lançamento no ledger)
curl -s -X POST http://localhost:8080/wallets \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"playerId":"018f2e6e-0000-7000-8000-000000000001","initialBalance":"1000.00","currency":"BRL"}'
# => {"id":"<walletId>", ...}

# 2. Submeter uma BET
curl -s -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{
    "externalTransactionId": "bet-1",
    "idempotencyKey": "provider-a:bet-1",
    "playerId": "018f2e6e-0000-7000-8000-000000000001",
    "walletId": "<walletId>",
    "roundId": "round-1",
    "gameId": "fortune-chimp",
    "kind": "BET",
    "amount": "25.00",
    "currency": "BRL"
  }'

# 3. Reenviar exatamente a mesma requisição: replay idempotente, sem débito duplicado
#    (mesmo status na faixa 200, "idempotentReplay": true)

# 4. Ler a carteira e seu ledger
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8080/wallets/<walletId>
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8080/wallets/<walletId>/ledger

# 5. Buscar a transação pelo seu id externo (com escopo por provedor: um token
#    do provider-b receberia um 403 aqui)
curl -s -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/providers/provider-a/wagering/transactions/bet-1

# 6. Reconciliar: recalcular o saldo a partir do ledger e comparar com o armazenado
curl -s -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8080/wallets/<walletId>/reconciliation

# 7. Health checks (sem autenticação)
curl -s http://localhost:8080/health/live
curl -s http://localhost:8080/health/ready
```

Um REFUND/ROLLBACK segue o mesmo formato de `POST /wagering/transactions`
com `"kind": "REFUND"` (ou `"ROLLBACK"`) e
`"referenceExternalTransactionId": "bet-1"`. Submeter um antes que a BET
referenciada exista não é um erro: a transação fica estacionada em
`PENDING_REFERENCE` e é resolvida automaticamente assim que a BET chegar
(ou rejeitada com `REFERENCE_NOT_FOUND` após o orçamento de retentativas
se esgotar — veja ARCHITECTURE.md §4/§10).

### Submetendo via RabbitMQ em vez de HTTP

Publique um corpo JSON com o mesmo formato da requisição HTTP acima
(mantendo `currency`/`amount` como obrigatórios) na exchange
`wager.events` com routing key `wager.transaction.<qualquer coisa>`, um
`MessageId` durável definido nas propriedades da mensagem AMQP
(obrigatório — veja ARCHITECTURE.md §5.2), e um header `x-provider-id`
nomeando o provedor (este é o equivalente, no transporte de mensagens,
ao bearer token da camada HTTP; veja o `singleProviderResolver` em
`internal/infra/fxmodules/rabbitmq.go` para entender como esse header é
resolvido atualmente, e seu comentário de documentação para ver como um
deployment de produção poderia substituí-lo por uma fila por provedor ou
por um gateway autenticado).

## Executando os testes

```sh
# Testes unitários de domínio + aplicação, com o detector de race (obrigatório --
# é isso que comprova as garantias de concorrência do ARCHITECTURE.md §4)
go test ./internal/domain/... ./internal/application/... -race

# Cobertura (veja ARCHITECTURE.md §11 para os números que isso produz atualmente)
go test ./internal/domain/... ./internal/application/... \
  -coverprofile=/tmp/cov.out -coverpkg=./internal/domain/...,./internal/application/...
go tool cover -func=/tmp/cov.out | tail -1

# Tudo, incluindo os pacotes de infra (compilados, mas não exercitados
# contra infraestrutura real neste sandbox -- veja ARCHITECTURE.md §11)
go test ./...
```

## Estrutura do projeto

```
cmd/api                        ponto de entrada do processo (fx.New(...).Run())
internal/domain                 Wallet, WagerTransaction, LedgerEntry, Money, events -- Go puro
internal/ports                  interfaces das quais a camada de aplicação depende
internal/application             casos de uso: ProcessWagerTransaction, OpenWallet,
                                  ResolvePendingReferences, Reconciliation, QueryService
internal/infra/postgres          implementações em pgx de cada interface de ports
internal/infra/rabbitmq          consumer, worker publicador de outbox, topologia/DLQ
internal/infra/keycloak          middleware de autenticação por bearer token JWT/JWKS
internal/infra/http               roteador chi + handlers
internal/infra/transport          o DTO de requisição compartilhado entre HTTP/RabbitMQ
internal/infra/config             carregamento de variáveis de ambiente
internal/infra/fxmodules          wiring do Uber Fx para todos os pacotes acima
migrations                       migrações SQL (compatíveis com golang-migrate)
deployments/keycloak              realm-export.json auto-provisionado pelo docker-compose
docker-compose.yml / Dockerfile
```

## Variáveis de ambiente

Veja [.env.example](.env.example) para a lista completa e oficial —
`internal/infra/config/config.go` é o único lugar que as lê.
