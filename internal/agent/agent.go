package agent

import (
	"Calc_2GO/models"
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	pb "Calc_2GO/api"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Agent struct {
	grpcClient         pb.CalculatorClient
	orchestratorAddr   string
	computingPower     int
	timeAddition       time.Duration
	timeSubtraction    time.Duration
	timeMultiplication time.Duration
	timeDivision       time.Duration
	logger             *log.Logger
	taskQueue          chan *models.Task
	wg                 sync.WaitGroup
}

func NewAgent(orchestratorAddr string, computingPower int) *Agent {
	os.Setenv("TIME_ADDITION_MS", "10000")
	os.Setenv("TIME_SUBTRACTION_MS", "10000")
	os.Setenv("TIME_MULTIPLICATION_MS", "10000")
	os.Setenv("TIME_DIVISION_MS", "10000")

	if computingPower <= 0 {
		computingPower = 1
	}

	logger := log.New(os.Stdout, "[AGENT] ", log.LstdFlags)

	timeAddition := getEnvDuration("TIME_ADDITION_MS")
	timeSubtraction := getEnvDuration("TIME_SUBTRACTION_MS")
	timeMultiplication := getEnvDuration("TIME_MULTIPLICATION_MS")
	timeDivision := getEnvDuration("TIME_DIVISION_MS")

	conn, err := grpc.Dial(orchestratorAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Fatalf("Failed to connect to orchestrator: %v", err)
	}

	return &Agent{
		grpcClient:         pb.NewCalculatorClient(conn),
		orchestratorAddr:   orchestratorAddr,
		computingPower:     computingPower,
		timeAddition:       timeAddition,
		timeSubtraction:    timeSubtraction,
		timeMultiplication: timeMultiplication,
		timeDivision:       timeDivision,
		logger:             logger,
		taskQueue:          make(chan *models.Task, computingPower),
	}
}

func (a *Agent) Start() {
	a.logger.Printf("Агент запущен, подключается к оркестратору по адресу: %s", a.orchestratorAddr)
	for i := 0; i < a.computingPower; i++ {
		go a.worker(i)
	}

	go a.taskDispatcher()
}

func (a *Agent) taskDispatcher() {
	for {
		task, err := a.getTask()
		if err != nil {
			a.logger.Printf("❌ ошибка при получении задачи: %v\n", err)
			time.Sleep(2 * time.Second)
			continue
		}

		a.wg.Add(1)
		a.taskQueue <- task
		a.wg.Wait()
	}
}

func (a *Agent) worker(id int) {
	for task := range a.taskQueue {
		a.logger.Printf("🔧 Агент %d начал выполнение задачи %d: %.2f %s %.2f",
			id, task.ID, task.Arg1, task.Operation, task.Arg2)

		result, err := a.ExecuteTask(task)
		if err != nil {
			a.logger.Printf("❌ Ошибка выполнения задачи %d: %v", task.ID, err)
			a.taskQueue <- task
			continue
		}

		a.logger.Printf("✅ Агент %d завершил задачу %d с результатом: %.2f",
			id, task.ID, result)

		if err := a.submitTaskResult(task.ID, result); err != nil {
			a.logger.Printf("❌ Ошибка отправки результата для задачи %d: %v", task.ID, err)
			a.taskQueue <- task
		} else {
			a.logger.Printf("📤 Результат задачи %d успешно отправлен: %.2f", task.ID, result)
		}

		a.wg.Done()
	}
}

func (a *Agent) getTask() (*models.Task, error) {
	resp, err := a.grpcClient.GetTask(context.Background(), &pb.TaskRequest{})
	if err != nil {
		return nil, fmt.Errorf("ошибка gRPC: %w", err)
	}
	return &models.Task{
		ID:        int(resp.Id),
		Arg1:      resp.Arg1,
		Arg2:      resp.Arg2,
		Operation: resp.Operation,
	}, nil
}

func (a *Agent) ExecuteTask(task *models.Task) (float64, error) {
	resolveArg := func(arg float64) (float64, error) {
		if arg >= 1000 { // Это ID подзадачи
			maxAttempts := 3
			for attempt := 1; attempt <= maxAttempts; attempt++ {
				time.Sleep(1 * time.Second)
				result, err := a.getTaskResult(int(arg))
				if err == nil {
					return result, nil
				}
				a.logger.Printf("Попытка %d/%d: не удалось получить результат подзадачи %d: %v",
					attempt, maxAttempts, int(arg), err)
			}
			return 0, fmt.Errorf("не удалось получить результат подзадачи %d после %d попыток",
				int(arg), maxAttempts)
		}
		return arg, nil
	}

	arg1, err := resolveArg(task.Arg1)
	if err != nil {
		return 0, err
	}

	arg2, err := resolveArg(task.Arg2)
	if err != nil {
		return 0, err
	}

	switch task.Operation {
	case "+":
		time.Sleep(a.timeAddition)
		return arg1 + arg2, nil
	case "-":
		time.Sleep(a.timeSubtraction)
		return arg1 - arg2, nil
	case "*":
		time.Sleep(a.timeMultiplication)
		return arg1 * arg2, nil
	case "/":
		time.Sleep(a.timeDivision)
		if arg2 == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		return arg1 / arg2, nil
	default:
		return 0, fmt.Errorf("unknown operation: %s", task.Operation)
	}
}

func (a *Agent) submitTaskResult(taskID int, result float64) error {
	_, err := a.grpcClient.SubmitResult(context.Background(), &pb.Result{
		Id:     int32(taskID),
		Result: result,
	})
	return err
}

func (a *Agent) getTaskResult(taskID int) (float64, error) {
	resp, err := a.grpcClient.GetTaskResult(context.Background(), &pb.TaskResultRequest{
		TaskId: int32(taskID),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get task result via gRPC: %w", err)
	}
	return resp.Result, nil
}

func getEnvDuration(key string) time.Duration {
	var defaultValue time.Duration = 2000 * time.Millisecond
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	ms, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return time.Duration(ms) * time.Millisecond
}
func (a *Agent) GetGRPCClient() pb.CalculatorClient {
	return a.grpcClient
}

// NewAgentWithClient создает агента с заданным gRPC клиентом (для тестов)
func NewAgentWithClient(client pb.CalculatorClient, computingPower int) *Agent {
	os.Setenv("TIME_ADDITION_MS", "10")
	os.Setenv("TIME_SUBTRACTION_MS", "10")
	os.Setenv("TIME_MULTIPLICATION_MS", "10")
	os.Setenv("TIME_DIVISION_MS", "10")

	if computingPower <= 0 {
		computingPower = 1
	}

	logger := log.New(os.Stdout, "[AGENT] ", log.LstdFlags)

	return &Agent{
		grpcClient:         client,
		computingPower:     computingPower,
		timeAddition:       getEnvDuration("TIME_ADDITION_MS"),
		timeSubtraction:    getEnvDuration("TIME_SUBTRACTION_MS"),
		timeMultiplication: getEnvDuration("TIME_MULTIPLICATION_MS"),
		timeDivision:       getEnvDuration("TIME_DIVISION_MS"),
		logger:             logger,
		taskQueue:          make(chan *models.Task, computingPower),
	}
}

// SetGRPCClient устанавливает gRPC клиент (для тестов)
func (a *Agent) SetGRPCClient(client pb.CalculatorClient) {
	a.grpcClient = client
}
