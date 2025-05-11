package calculator_test

import (
	"Calc_2GO/models"
	"Calc_2GO/pkg/calculator"
	"fmt"
	"testing"
	"time"
)

func TestCalcToTasks(t *testing.T) {
	tests := []struct {
		name        string
		expression  string
		wantTasks   []models.Task
		wantTaskIDs []int
		wantErr     bool
		errMsg      string
	}{
		{
			name:       "Простая операция сложения",
			expression: "2+2",
			wantTasks: []models.Task{
				{ID: 1001, Arg1: 2, Arg2: 2, Operation: "+", OperationTime: time.Second},
			},
			wantTaskIDs: []int{1001},
			wantErr:     false,
		},
		{
			name:       "Приоритет операций",
			expression: "2+2*2",
			wantTasks: []models.Task{
				{ID: 1001, Arg1: 2, Arg2: 2, Operation: "*", OperationTime: time.Second},
				{ID: 1002, Arg1: 2, Arg2: 1001, Operation: "+", OperationTime: time.Second},
			},
			wantTaskIDs: []int{1001, 1002},
			wantErr:     false,
		},
		{
			name:       "Деление на ноль",
			expression: "10/0",
			wantTasks: []models.Task{
				{ID: 1001, Arg1: 10, Arg2: 0, Operation: "/", OperationTime: time.Second},
			},
			wantTaskIDs: []int{1001},
			wantErr:     false, // Ошибка деления на ноль будет при выполнении, а не при разборе
		},
		{
			name:       "Неверный символ",
			expression: "2 + a",
			wantErr:    true,
			errMsg:     "invalid character: a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Используем фиксированный ID выражения для предсказуемости тестов
			tasks, err := calculator.CalcToTasks(1, tt.expression)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("❌ %s: ожидалась ошибка, но не получена", tt.name)
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Fatalf("❌ %s: ожидалась ошибка '%s', получена '%s'", tt.name, tt.errMsg, err.Error())
				}
				fmt.Printf("✅ %s: корректная ошибка '%s'\n", tt.name, err.Error())
				return
			}

			if err != nil {
				t.Fatalf("❌ %s: неожиданная ошибка: %v", tt.name, err)
			}

			if len(tasks) != len(tt.wantTasks) {
				t.Fatalf("❌ %s: ожидалось %d задач, получено %d", tt.name, len(tt.wantTasks), len(tasks))
			}

			for i, task := range tasks {
				if task.ID != tt.wantTaskIDs[i] {
					t.Errorf("❌ %s: задача %d - ожидался ID %d, получен %d",
						tt.name, i, tt.wantTaskIDs[i], task.ID)
				}
				if task.Operation != tt.wantTasks[i].Operation {
					t.Errorf("❌ %s: задача %d - ожидалась операция '%s', получена '%s'",
						tt.name, i, tt.wantTasks[i].Operation, task.Operation)
				}
			}

			fmt.Printf("✅ %s: успешно создано %d задач\n", tt.name, len(tasks))
		})

	}

}
