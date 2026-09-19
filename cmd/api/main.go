package main

import (
	"go.uber.org/fx"

	"github.com/cassianobraz/wager-service/internal/infra/fxmodules"
)

func main() {
	fx.New(fxmodules.Module()).Run()
}
