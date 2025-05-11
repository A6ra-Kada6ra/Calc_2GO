package orchestrator_test

import (
	"Calc_2GO/internal/orchestrator"
	"Calc_2GO/models"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrchestrator_UserOperations(t *testing.T) {
	// Подготовка тестового окружения
	dbFile := "test_users.db"
	os.Remove(dbFile)
	defer os.Remove(dbFile)

	o := orchestrator.NewOrchestrator()

	t.Run("Регистрация и аутентификация пользователя", func(t *testing.T) {
		// Тестируем регистрацию
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/register", strings.NewReader(
			`{"login":"testuser","password":"testpass"}`,
		))
		o.HandleRegister(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)

		// Тестируем вход
		w = httptest.NewRecorder()
		req = httptest.NewRequest("POST", "/api/v1/login", strings.NewReader(
			`{"login":"testuser","password":"testpass"}`,
		))
		o.HandleLogin(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Проверяем токен
		var resp map[string]string
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.NotEmpty(t, resp["token"])
	})

	t.Run("Ошибка дублирования пользователя", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/register", strings.NewReader(
			`{"login":"testuser","password":"testpass"}`,
		))
		o.HandleRegister(w, req)
		assert.Equal(t, http.StatusConflict, w.Code)
	})
}

func TestOrchestrator_ExpressionHandling(t *testing.T) {
	dbFile := "test_expressions.db"
	os.Remove(dbFile)
	defer os.Remove(dbFile)

	o := orchestrator.NewOrchestrator()

	// Создаем тестового пользователя
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/register", strings.NewReader(
		`{"login":"expruser","password":"exprpass"}`,
	))
	o.HandleRegister(w, req)

	// Получаем токен
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/login", strings.NewReader(
		`{"login":"expruser","password":"exprpass"}`,
	))
	o.HandleLogin(w, req)
	var loginResp map[string]string
	json.Unmarshal(w.Body.Bytes(), &loginResp)
	token := loginResp["token"]

	t.Run("Добавление и получение выражений", func(t *testing.T) {
		// Добавляем выражение
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/calculate", strings.NewReader(
			`{"expression":"2+2*2"}`,
		))
		req.Header.Set("Authorization", "Bearer "+token)
		o.AuthMiddleware(o.HandleCalculate)(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)

		var resp map[string]string
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		exprID := resp["id"]
		require.NotEmpty(t, exprID)

		// Получаем список выражений
		w = httptest.NewRecorder()
		req = httptest.NewRequest("GET", "/api/v1/expressions", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		o.AuthMiddleware(o.HandleGetExpressions)(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var exprs []models.Expression
		err = json.Unmarshal(w.Body.Bytes(), &exprs)
		require.NoError(t, err)
		assert.Len(t, exprs, 1)
		assert.Equal(t, "2+2*2", exprs[0].Expression)
	})

	t.Run("Обработка неверного выражения", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/v1/calculate", strings.NewReader(
			`{"expression":"2+"}`,
		))
		req.Header.Set("Authorization", "Bearer "+token)
		o.AuthMiddleware(o.HandleCalculate)(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestOrchestrator_TaskProcessing(t *testing.T) {
	dbFile := "test_tasks.db"
	os.Remove(dbFile)
	defer os.Remove(dbFile)

	o := orchestrator.NewOrchestrator()

	// Добавляем тестовое выражение
	exprID, err := o.AddExpression(1, "2+3*4")
	require.NoError(t, err)

	t.Run("Получение и выполнение задач", func(t *testing.T) {
		// Получаем первую задачу (3*4)
		task, exists := o.GetNextTask()
		require.True(t, exists)
		assert.Equal(t, "*", task.Operation)

		// Имитируем обработку задачи
		result := task.Arg1 * task.Arg2
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/internal/task", strings.NewReader(
			fmt.Sprintf(`{"id":%d,"result":%f}`, task.ID, result),
		))
		o.HandleTaskResult(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Получаем вторую задачу (2+12)
		task, exists = o.GetNextTask()
		require.True(t, exists)
		assert.Equal(t, "+", task.Operation)

		// Имитируем обработку второй задачи
		result = task.Arg1 + task.Arg2
		w = httptest.NewRecorder()
		req = httptest.NewRequest("POST", "/internal/task", strings.NewReader(
			fmt.Sprintf(`{"id":%d,"result":%f}`, task.ID, result),
		))
		o.HandleTaskResult(w, req)

		// Проверяем результат выражения
		expr, exists := o.GetExpression(exprID)
		require.True(t, exists)
		assert.Equal(t, "done", expr.Status)
		assert.Equal(t, 14.0, expr.Result)
	})
}

func TestOrchestrator_Concurrency(t *testing.T) {
	dbFile := "test_concurrency.db"
	os.Remove(dbFile)
	defer os.Remove(dbFile)

	o := orchestrator.NewOrchestrator()

	// Добавляем несколько выражений
	for i := 0; i < 5; i++ {
		_, err := o.AddExpression(1, fmt.Sprintf("%d+%d", i, i+1))
		require.NoError(t, err)
	}

	var wg sync.WaitGroup
	workerCount := 3
	wg.Add(workerCount)

	// Запускаем несколько "воркеров" для обработки задач
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			for {
				task, exists := o.GetNextTask()
				if !exists {
					return
				}

				// Имитируем обработку задачи
				result := task.Arg1 + task.Arg2
				w := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/internal/task", strings.NewReader(
					fmt.Sprintf(`{"id":%d,"result":%f}`, task.ID, result),
				))
				o.HandleTaskResult(w, req)
			}
		}()
	}

	wg.Wait()

	// Проверяем, что все выражения выполнены
	exprs := o.GetUserExpressions(1)
	for _, expr := range exprs {
		assert.Equal(t, "done", expr.Status)
	}
}
