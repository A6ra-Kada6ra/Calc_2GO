package agent_test

import (
	"Calc_2GO/models"
	"context"
	"errors"
	"testing"
	"time"

	pb "Calc_2GO/api"
	"Calc_2GO/internal/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"
)

// MockCalculatorClient заменяет реальный gRPC клиент для тестов
type MockCalculatorClient struct {
	mock.Mock
}

func (m *MockCalculatorClient) GetTask(ctx context.Context, in *pb.TaskRequest, opts ...grpc.CallOption) (*pb.Task, error) {
	args := m.Called(ctx, in)
	return args.Get(0).(*pb.Task), args.Error(1)
}

func (m *MockCalculatorClient) SubmitResult(ctx context.Context, in *pb.Result, opts ...grpc.CallOption) (*pb.ResultResponse, error) {
	args := m.Called(ctx, in)
	return args.Get(0).(*pb.ResultResponse), args.Error(1)
}

func (m *MockCalculatorClient) GetTaskResult(ctx context.Context, in *pb.TaskResultRequest, opts ...grpc.CallOption) (*pb.TaskResultResponse, error) {
	args := m.Called(ctx, in)
	return args.Get(0).(*pb.TaskResultResponse), args.Error(1)
}

// TestAgent_ExecuteTask проверяет выполнение арифметических операций
func TestAgent_ExecuteTask(t *testing.T) {
	tests := []struct {
		name       string
		task       models.Task
		expected   float64
		wantError  bool
		errMessage string
	}{
		{
			name:      "Сложение",
			task:      models.Task{ID: 1, Arg1: 2, Arg2: 3, Operation: "+", OperationTime: 10 * time.Millisecond},
			expected:  5,
			wantError: false,
		},
		{
			name:      "Вычитание",
			task:      models.Task{ID: 2, Arg1: 5, Arg2: 3, Operation: "-", OperationTime: 10 * time.Millisecond},
			expected:  2,
			wantError: false,
		},
		{
			name:      "Умножение",
			task:      models.Task{ID: 3, Arg1: 2, Arg2: 3, Operation: "*", OperationTime: 10 * time.Millisecond},
			expected:  6,
			wantError: false,
		},
		{
			name:      "Деление",
			task:      models.Task{ID: 4, Arg1: 6, Arg2: 3, Operation: "/", OperationTime: 10 * time.Millisecond},
			expected:  2,
			wantError: false,
		},
		{
			name:       "Деление на ноль",
			task:       models.Task{ID: 5, Arg1: 6, Arg2: 0, Operation: "/", OperationTime: 10 * time.Millisecond},
			wantError:  true,
			errMessage: "division by zero",
		},
		{
			name:       "Неизвестная операция",
			task:       models.Task{ID: 6, Arg1: 2, Arg2: 3, Operation: "^", OperationTime: 10 * time.Millisecond},
			wantError:  true,
			errMessage: "unknown operation: ^",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := new(MockCalculatorClient)
			ag := agent.NewAgent("localhost:50051", 1)

			// Для тестов подменяем gRPC клиент
			ag.SetGRPCClient(mockClient)

			// Замеряем время выполнения для проверки задержек
			start := time.Now()
			result, err := ag.ExecuteTask(&tt.task)
			duration := time.Since(start)

			if tt.wantError {
				assert.Error(t, err)
				if tt.errMessage != "" {
					assert.Contains(t, err.Error(), tt.errMessage)
				}
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result)

			// Проверяем что операция заняла примерно ожидаемое время
			minExpected := tt.task.OperationTime - 5*time.Millisecond
			assert.GreaterOrEqual(t, duration, minExpected, "operation completed too fast")
		})
	}
}

// TestAgent_TaskHandling проверяет обработку задач через gRPC
func TestAgent_TaskHandling(t *testing.T) {
	mockClient := new(MockCalculatorClient)
	ag := agent.NewAgent("localhost:50051", 1)
	ag.SetGRPCClient(mockClient)

	// Настраиваем ожидания для получения задачи
	expectedTask := &pb.Task{
		Id:        1,
		Arg1:      2,
		Arg2:      3,
		Operation: "+",
	}
	mockClient.On("GetTask", mock.Anything, &pb.TaskRequest{}).
		Return(expectedTask, nil).Once()

	// Настраиваем ожидания для отправки результата
	mockClient.On("SubmitResult", mock.Anything, &pb.Result{
		Id:     1,
		Result: 5.0,
	}).Return(&pb.ResultResponse{Success: true}, nil).Once()

	// Тестируем получение задачи
	task, err := ag.GetTask()
	assert.NoError(t, err)
	assert.Equal(t, int(expectedTask.Id), task.ID)
	assert.Equal(t, expectedTask.Arg1, task.Arg1)
	assert.Equal(t, expectedTask.Arg2, task.Arg2)
	assert.Equal(t, expectedTask.Operation, task.Operation)

	// Тестируем отправку результата
	err = ag.SubmitTaskResult(task.ID, 5.0)
	assert.NoError(t, err)

	mockClient.AssertExpectations(t)
}

// TestAgent_ErrorHandling проверяет обработку ошибок
func TestAgent_ErrorHandling(t *testing.T) {
	mockClient := new(MockCalculatorClient)
	ag := agent.NewAgent("localhost:50051", 1)
	ag.SetGRPCClient(mockClient)

	// Тест ошибки при получении задачи
	mockClient.On("GetTask", mock.Anything, &pb.TaskRequest{}).
		Return((*pb.Task)(nil), errors.New("connection error")).Once()

	_, err := ag.GetTask()
	assert.Error(t, err)

	// Тест ошибки при отправке результата
	mockClient.On("SubmitResult", mock.Anything, mock.Anything).
		Return((*pb.ResultResponse)(nil), errors.New("submit error")).Once()

	err = ag.SubmitTaskResult(1, 5.0)
	assert.Error(t, err)

	mockClient.AssertExpectations(t)
}
