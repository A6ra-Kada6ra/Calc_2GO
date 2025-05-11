package main

import (
	"Calc_2GO/internal/agent"
	"log"
)

func main() {
	orchestratorAddr := "localhost:50051"
	computingPower := 2

	agent := agent.NewAgent(orchestratorAddr, computingPower)
	log.Println("🚀 Запуск агента...")
	agent.Start()

	select {}
}
