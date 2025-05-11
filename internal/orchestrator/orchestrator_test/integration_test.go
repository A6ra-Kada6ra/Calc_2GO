package orchestrator_test

import (
	"Calc_2GO/internal/orchestrator"
	"Calc_2GO/models"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	pb "Calc_2GO/api"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestFullIntegration(t *testing.T) {
	// Настройка тестового окружения
	dbFile := "test_integration.db"
	os.Remove(dbFile)
	defer os.Remove(dbFile)

	// Запускаем оркестратор
	o := orchestrator.NewOrchestrator()

	// Настраиваем HTTP обработчики
	http.HandleFunc("/api/v1/register", o.HandleRegister)
	http.HandleFunc("/api/v1/login", o.HandleLogin)
	http.HandleFunc("/api/v1/calculate", o.AuthMiddleware(o.HandleCalculate))
	http.HandleFunc("/api/v1/expressions", o.AuthMiddleware(o.HandleGetExpressions))
	http.HandleFunc("/api/v1/expressions/", o.AuthMiddleware(o.HandleGetExpressionByID))

	// Запускаем gRPC сервер в отдельной горутине
	grpcLis, err := net.Listen("tcp", ":50051")
	require.NoError(t, err)
	grpcServer := grpc.NewServer()
	calcServer := &orchestrator.CalculatorServer{} // Используем конструктор по умолчанию
	calcServer.SetOrchestrator(o)                  // Устанавливаем оркестратор через метод
	pb.RegisterCalculatorServer(grpcServer, calcServer)
	go grpcServer.Serve(grpcLis)
	defer grpcServer.Stop()

	// Запускаем HTTP сервер в отдельной горутине
	go func() {
		if err := http.ListenAndServe(":8080", nil); err != nil {
			t.Logf("HTTP server error: %v", err)
		}
	}()

	// Даем серверам время на запуск
	time.Sleep(500 * time.Millisecond)

	// 1. Тестируем регистрацию пользователя
	registerResp := makeHTTPRequest(t, "POST", "http://localhost:8080/api/v1/register", map[string]interface{}{
		"login":    "testuser",
		"password": "testpass",
	})
	require.Equal(t, http.StatusCreated, registerResp.StatusCode)

	// 2. Тестируем аутентификацию
	loginResp := makeHTTPRequest(t, "POST", "http://localhost:8080/api/v1/login", map[string]interface{}{
		"login":    "testuser",
		"password": "testpass",
	})
	require.Equal(t, http.StatusOK, loginResp.StatusCode)

	loginBody, err := io.ReadAll(loginResp.Body)
	require.NoError(t, err)

	var loginData map[string]string
	require.NoError(t, json.Unmarshal(loginBody, &loginData))
	token := loginData["token"]
	require.NotEmpty(t, token)

	// 3. Добавляем выражение через HTTP API
	calcResp := makeHTTPRequest(t, "POST", "http://localhost:8080/api/v1/calculate", map[string]interface{}{
		"expression": "2+2*2",
	}, token)
	require.Equal(t, http.StatusCreated, calcResp.StatusCode)

	calcBody, err := io.ReadAll(calcResp.Body)
	require.NoError(t, err)

	var calcData map[string]string
	require.NoError(t, json.Unmarshal(calcBody, &calcData))
	exprID := calcData["id"]
	require.NotEmpty(t, exprID)

	// 4. Обрабатываем задачи через gRPC
	conn, err := grpc.Dial("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	client := pb.NewCalculatorClient(conn)

	// Обрабатываем все задачи
	for i := 0; i < 2; i++ {
		task, err := client.GetTask(context.Background(), &pb.TaskRequest{})
		if err != nil {
			break
		}

		var result float64
		switch task.Operation {
		case "+":
			result = task.Arg1 + task.Arg2
		case "*":
			result = task.Arg1 * task.Arg2
		}

		_, err = client.SubmitResult(context.Background(), &pb.Result{
			Id:     task.Id,
			Result: result,
		})
		require.NoError(t, err)
	}

	// 5. Проверяем результат через HTTP API
	time.Sleep(500 * time.Millisecond) // Даем время на обработку

	exprResp := makeHTTPRequest(t, "GET", "http://localhost:8080/api/v1/expressions/"+exprID, nil, token)
	require.Equal(t, http.StatusOK, exprResp.StatusCode)

	exprBody, err := io.ReadAll(exprResp.Body)
	require.NoError(t, err)

	var expr models.Expression
	require.NoError(t, json.Unmarshal(exprBody, &expr))
	require.Equal(t, "done", expr.Status)
	require.Equal(t, 6.0, expr.Result)
}

func TestGRPCIntegration(t *testing.T) {
	dbFile := "test_grpc_integration.db"
	os.Remove(dbFile)
	defer os.Remove(dbFile)

	o := orchestrator.NewOrchestrator()

	// Запускаем gRPC сервер
	lis, err := net.Listen("tcp", ":50051")
	require.NoError(t, err)
	srv := grpc.NewServer()
	calcServer := &orchestrator.CalculatorServer{} // Используем конструктор по умолчанию
	calcServer.SetOrchestrator(o)                  // Устанавливаем оркестратор через метод
	pb.RegisterCalculatorServer(srv, calcServer)
	go srv.Serve(lis)
	defer srv.Stop()

	// Даем серверу время на запуск
	time.Sleep(100 * time.Millisecond)

	// Подключаемся к серверу
	conn, err := grpc.Dial("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	client := pb.NewCalculatorClient(conn)

	// Добавляем тестовое выражение
	exprID, err := o.AddExpression(1, "3+4*5")
	require.NoError(t, err)

	// Получаем и обрабатываем задачи
	for i := 0; i < 2; i++ {
		task, err := client.GetTask(context.Background(), &pb.TaskRequest{})
		require.NoError(t, err)

		var result float64
		switch task.Operation {
		case "+":
			result = task.Arg1 + task.Arg2
		case "*":
			result = task.Arg1 * task.Arg2
		}

		_, err = client.SubmitResult(context.Background(), &pb.Result{
			Id:     task.Id,
			Result: result,
		})
		require.NoError(t, err)
	}

	// Проверяем результат
	expr, exists := o.GetExpression(exprID)
	require.True(t, exists)
	assert.Equal(t, "done", expr.Status)
	assert.Equal(t, 23.0, expr.Result)
}

// Вспомогательная функция для HTTP запросов
func makeHTTPRequest(t *testing.T, method, url string, body interface{}, token ...string) *http.Response {
	var reqBody *strings.Reader
	if body != nil {
		jsonBody, _ := json.Marshal(body)
		reqBody = strings.NewReader(string(jsonBody))
	}

	req, err := http.NewRequest(method, url, reqBody)
	require.NoError(t, err)

	if len(token) > 0 {
		req.Header.Set("Authorization", "Bearer "+token[0])
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)

	return resp
}
