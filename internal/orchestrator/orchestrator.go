package orchestrator

import (
	"Calc_2GO/models"
	"Calc_2GO/pkg/calculator"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "Calc_2GO/api"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Orchestrator struct {
	storage     *Storage
	mu          sync.Mutex
	jwtSecret   []byte
	taskResults map[int]float64
}

type CalculatorServer struct {
	pb.UnimplementedCalculatorServer
	orchestrator *Orchestrator
}

func (s *CalculatorServer) GetTask(ctx context.Context, req *pb.TaskRequest) (*pb.Task, error) {
	task, exists := s.orchestrator.GetNextTask()
	if !exists {
		return nil, status.Errorf(codes.NotFound, "🔍 Нет доступных задач")
	}

	log.Printf("📦 Отправлена задача %d: %.2f %s %.2f", task.ID, task.Arg1, task.Operation, task.Arg2)
	return &pb.Task{
		Id:        int32(task.ID),
		Arg1:      task.Arg1,
		Arg2:      task.Arg2,
		Operation: task.Operation,
	}, nil
}

func (s *CalculatorServer) SubmitResult(ctx context.Context, res *pb.Result) (*pb.ResultResponse, error) {
	log.Printf("📩 Получен результат для задачи %d: %.2f", res.Id, res.Result)

	// Сохраняем результат подзадачи
	if err := s.orchestrator.storage.SaveTaskResult(int(res.Id), res.Result); err != nil {
		log.Printf("❌ Ошибка сохранения результата: %v", err)
		return nil, status.Errorf(codes.Internal, "🚨 Ошибка сохранения результата")
	}

	// Обновляем статус задачи
	if err := s.orchestrator.storage.UpdateTaskStatus(int(res.Id), "done"); err != nil {
		log.Printf("❌ Ошибка обновления статуса: %v", err)
		return nil, status.Errorf(codes.Internal, "🚨 Ошибка обновления статуса задачи")
	}

	// Проверяем завершение всех подзадач
	exprID := int(res.Id) / 1000
	tasks, err := s.orchestrator.storage.GetTasksByExpressionID(exprID)
	if err != nil {
		log.Printf("❌ Ошибка получения подзадач: %v", err)
		return nil, status.Errorf(codes.Internal, "🚨 Ошибка проверки статусов задач")
	}

	allDone := true
	for _, task := range tasks {
		if task.Status != "done" {
			allDone = false
			break
		}
	}

	if allDone {
		if err := s.orchestrator.storage.UpdateExpressionStatus(exprID, "done", res.Result); err != nil {
			log.Printf("❌ Ошибка обновления выражения: %v", err)
			return nil, status.Errorf(codes.Internal, "🚨 Ошибка обновления статуса выражения")
		}
		log.Printf("🎉 Выражение %d завершено! Результат: %.2f", exprID, res.Result)
	}

	return &pb.ResultResponse{Success: true}, nil
}

func NewOrchestrator() *Orchestrator {
	return &Orchestrator{
		storage:     NewStorage(),
		jwtSecret:   []byte("my_super_secret_key"),
		taskResults: make(map[int]float64),
	}
}

func (o *Orchestrator) AddExpression(userID int, expr string) (int, error) {
	log.Printf("🛠️ Добавление выражения для userID %d: %s", userID, expr)
	id, err := o.storage.AddExpression(userID, expr)
	if err != nil {
		log.Printf("❌ Ошибка при добавлении выражения: %v", err)
		return 0, fmt.Errorf("🚨 Ошибка сохранения выражения")
	}

	tasks, err := calculator.CalcToTasks(id, expr)
	if err != nil {
		log.Printf("❌ Ошибка разбора выражения: %v", err)
		return 0, fmt.Errorf("🚨 Некорректное выражение")
	}

	for i := range tasks {
		tasks[i].UserID = userID
		log.Printf("📌 Создана задача: ID=%d, Arg1=%.2f, Arg2=%.2f, Op=%s",
			tasks[i].ID, tasks[i].Arg1, tasks[i].Arg2, tasks[i].Operation)
	}

	log.Printf("✅ Успешно создано %d подзадач для выражения ID %d", len(tasks), id)
	if err := o.storage.AddTasks(tasks); err != nil {
		log.Printf("❌ Ошибка сохранения задач: %v", err)
		return 0, fmt.Errorf("🚨 Ошибка сохранения задач")
	}

	log.Printf("🎯 Выражение ID %d успешно добавлено", id)
	return id, nil
}

func (s *CalculatorServer) GetTaskResult(ctx context.Context, req *pb.TaskResultRequest) (*pb.TaskResultResponse, error) {
	result, err := s.orchestrator.storage.GetTaskResult(int(req.TaskId))
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "🚨 Результат задачи не найден")
	}
	return &pb.TaskResultResponse{Result: result}, nil
}

func (o *Orchestrator) GetUserExpressions(userID int) []*models.Expression {
	exprs, err := o.storage.GetExpressions(userID)
	if err != nil {
		log.Printf("🚨 Ошибка получения выражений: %v", err)
		return []*models.Expression{}
	}
	return exprs
}

func (o *Orchestrator) GetExpression(id int) (*models.Expression, bool) {
	expr, err := o.storage.GetExpression(id)
	if err != nil {
		return nil, false
	}
	return expr, true
}

func (o *Orchestrator) GetNextTask() (*models.Task, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()

	task, err := o.storage.GetNextTask()
	if err != nil {
		log.Println("🔄 Нет задач, готовых к выполнению:", err)
		return nil, false
	}

	// Обновляем статус задачи
	err = o.storage.UpdateTaskStatus(task.ID, "in_progress")
	if err != nil {
		log.Printf("🚨 Ошибка обновления статуса задачи: %v", err)
		return nil, false
	}

	return task, true
}

func (o *Orchestrator) HandleGetExpressions(w http.ResponseWriter, r *http.Request) {
	// Получаем userID из токена
	claims := r.Context().Value("claims").(jwt.MapClaims)
	userID := int(claims["user_id"].(float64))

	expressions := o.GetUserExpressions(userID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(expressions)
}

func (o *Orchestrator) HandleGetExpressionByID(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/expressions/")

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "🚨 Неверный формат ID", http.StatusBadRequest)
		return
	}

	expr, exists := o.GetExpression(id)
	if !exists {
		http.Error(w, "🔍 Выражение не найдено", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(expr)
}

func (o *Orchestrator) HandleCalculate(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value("claims").(jwt.MapClaims)
	userID := int(claims["user_id"].(float64))

	var request struct {
		Expression string `json:"expression"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, fmt.Sprintf("🚨 Ошибка чтения данных: %v", err), http.StatusBadRequest)
		return
	}

	id, err := o.AddExpression(userID, request.Expression)
	if err != nil {
		http.Error(w, fmt.Sprintf("🚨 Ошибка добавления выражения: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": strconv.Itoa(id)})
}

func (o *Orchestrator) HandleTask(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		task, exists := o.GetNextTask()
		if !exists {
			http.Error(w, "🔄 Нет доступных задач", http.StatusNotFound)
			return
		}
		err := o.storage.UpdateExpressionStatus(task.ID, "in_progress", 0)
		if err != nil {
			log.Printf("🚨 Ошибка обновления статуса выражения: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(task)

	case http.MethodPost:
		o.HandleTaskResult(w, r)

	default:
		http.Error(w, "🚨 Метод не поддерживается", http.StatusMethodNotAllowed)
	}
}

func (o *Orchestrator) HandleTaskResult(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID     int     `json:"id"`
		Result float64 `json:"result"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "🚨 Неверный запрос", http.StatusBadRequest)
		return
	}

	err := o.storage.SaveTaskResult(request.ID, request.Result)
	if err != nil {
		http.Error(w, "🚨 Ошибка сохранения результата", http.StatusInternalServerError)
		return
	}

	err = o.storage.UpdateTaskStatus(request.ID, "done")
	if err != nil {
		http.Error(w, "🚨 Ошибка обновления статуса задачи", http.StatusInternalServerError)
		return
	}

	exprID := request.ID / 1000

	tasks, err := o.storage.GetTasksByExpressionID(exprID)
	if err != nil {
		http.Error(w, "🚨 Ошибка получения задач", http.StatusInternalServerError)
		return
	}

	allDone := true
	for _, task := range tasks {
		if task.Status != "done" {
			allDone = false
			break
		}
	}

	if allDone {
		finalResult := request.Result
		if err := o.storage.UpdateExpressionStatus(exprID, "done", finalResult); err != nil {
			http.Error(w, "🚨 Внутренняя ошибка сервера", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (o *Orchestrator) HandleRegister(w http.ResponseWriter, r *http.Request) {
	var req models.AuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "🚨 Неверный запрос", http.StatusBadRequest)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "🚨 Ошибка хеширования пароля", http.StatusInternalServerError)
		return
	}

	_, err = o.storage.CreateUser(req.Login, string(hashedPassword))
	if err != nil {
		http.Error(w, "🚨 Пользователь уже существует", http.StatusConflict)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "✅ Пользователь создан"})
}

func (o *Orchestrator) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req models.AuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "🚨 Неверный запрос", http.StatusBadRequest)
		return
	}

	user, err := o.storage.GetUserByLogin(req.Login)
	if err != nil {
		http.Error(w, "🔐 Неверные учетные данные", http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		http.Error(w, "🔐 Неверные учетные данные", http.StatusUnauthorized)
		return
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	})

	tokenString, err := token.SignedString(o.jwtSecret)
	if err != nil {
		http.Error(w, "🚨 Ошибка генерации токена", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"token": tokenString})
}

func (o *Orchestrator) HandleGetTaskResult(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/internal/task/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "🚨 Неверный ID задачи", http.StatusBadRequest)
		return
	}

	o.mu.Lock()
	result, exists := o.taskResults[id]
	o.mu.Unlock()

	if !exists {
		http.Error(w, "🔍 Результат задачи не найден", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]float64{"result": result})
}

func (o *Orchestrator) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "🔐 Требуется заголовок Authorization", http.StatusUnauthorized)
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			return o.jwtSecret, nil
		})

		if err != nil || !token.Valid {
			http.Error(w, "🔐 Неверный токен", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), "claims", token.Claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (o *Orchestrator) StartGRPCServer() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("🚨 Ошибка запуска gRPC сервера: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterCalculatorServer(s, &CalculatorServer{orchestrator: o})

	log.Println("🟢 gRPC сервер запущен на :50051")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("🚨 Ошибка работы gRPC сервера: %v", err)
	}
}

func (o *Orchestrator) StartServer() {
	if err := o.storage.db.Ping(); err != nil {
		log.Fatal("🚨 Ошибка подключения к базе данных:", err)
	}

	http.HandleFunc("/api/v1/register", o.HandleRegister)
	http.HandleFunc("/api/v1/login", o.HandleLogin)
	http.HandleFunc("/internal/task/", o.HandleGetTaskResult)

	http.HandleFunc("/api/v1/calculate", o.AuthMiddleware(o.HandleCalculate))
	http.HandleFunc("/api/v1/expressions", o.AuthMiddleware(o.HandleGetExpressions))
	http.HandleFunc("/api/v1/expressions/", o.AuthMiddleware(o.HandleGetExpressionByID))

	http.HandleFunc("/internal/task", o.HandleTask)

	log.Println("🟢 HTTP сервер запущен на :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal("🚨 Ошибка запуска HTTP сервера:", err)
	}
}
func (s *CalculatorServer) SetOrchestrator(o *Orchestrator) {
	s.orchestrator = o
}
