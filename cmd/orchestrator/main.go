package main

import (
	"Calc_2GO/internal/orchestrator"
	"log"
)

func main() {
	o := orchestrator.NewOrchestrator()
	go o.StartGRPCServer()
	log.Println("🛠️ Запуск оркестратора...")
	o.StartServer()
}
