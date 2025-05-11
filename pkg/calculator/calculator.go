package calculator

import (
	models "Calc_2GO/models"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidExpression = errors.New("invalid expression")
	ErrDivisionByZero    = errors.New("division by zero")
	ErrMismatchedParens  = errors.New("mismatched parentheses")
	ErrInvalidToken      = errors.New("invalid token")
	ErrInvalidCharacter  = errors.New("invalid character")
)

func CalcToTasks(id int, expression string) ([]models.Task, error) {
	if expression == "" {
		return nil, ErrInvalidExpression
	}

	tokens, err := tokenize(expression)
	if err != nil {
		return nil, err
	}

	postfix, err := infixToPostfix(tokens)
	if err != nil {
		return nil, err
	}

	tasks, err := evaluatePostfixToTasks(id, postfix)
	if err != nil {
		return nil, err
	}

	return tasks, nil
}

func tokenize(expr string) ([]string, error) {
	var tokens []string
	var currentToken strings.Builder

	for i, char := range expr {
		if char == ' ' {
			continue
		} else if char == '+' || char == '-' || char == '*' || char == '/' || char == '(' || char == ')' {
			if currentToken.Len() > 0 {
				tokens = append(tokens, currentToken.String())
				currentToken.Reset()
			}
			if char == '-' && (i == 0 || expr[i-1] == '(') {
				currentToken.WriteRune(char)
			} else {
				tokens = append(tokens, string(char))
			}
		} else {
			currentToken.WriteRune(char)
		}
	}

	if currentToken.Len() > 0 {
		tokens = append(tokens, currentToken.String())
	}

	return tokens, nil
}

func infixToPostfix(tokens []string) ([]string, error) {
	var output []string
	var operators []string

	for _, token := range tokens {
		if isNumber(token) {
			output = append(output, token)
		} else if token == "(" {
			operators = append(operators, token)
		} else if token == ")" {
			for len(operators) > 0 && operators[len(operators)-1] != "(" {
				output = append(output, operators[len(operators)-1])
				operators = operators[:len(operators)-1]
			}
			if len(operators) == 0 {
				return nil, ErrMismatchedParens
			}
			operators = operators[:len(operators)-1]
		} else if isOperator(token) {
			for len(operators) > 0 && precedence(operators[len(operators)-1]) >= precedence(token) {
				output = append(output, operators[len(operators)-1])
				operators = operators[:len(operators)-1]
			}
			operators = append(operators, token)
		} else {
			return nil, fmt.Errorf("%w: %s", ErrInvalidCharacter, token)
		}
	}

	for len(operators) > 0 {
		if operators[len(operators)-1] == "(" {
			return nil, ErrMismatchedParens
		}
		output = append(output, operators[len(operators)-1])
		operators = operators[:len(operators)-1]
	}

	return output, nil
}

func evaluatePostfixToTasks(id int, postfix []string) ([]models.Task, error) {
	var stack []interface{}
	var tasks []models.Task
	var subTaskID = 0

	for _, token := range postfix {
		if isNumber(token) {
			num, _ := strconv.ParseFloat(token, 64)
			stack = append(stack, num)
		} else if isOperator(token) {
			if len(stack) < 2 {
				return nil, ErrInvalidExpression
			}

			arg2 := stack[len(stack)-1]
			arg1 := stack[len(stack)-2]
			stack = stack[:len(stack)-2]

			subTaskID++
			taskID := id*1000 + subTaskID

			task := models.Task{
				ID:            taskID,
				Operation:     token,
				OperationTime: time.Second,
			}

			if f, ok := arg1.(float64); ok {
				task.Arg1 = f
			} else if taskID, ok := arg1.(int); ok {
				task.Arg1 = float64(taskID)
			}

			if f, ok := arg2.(float64); ok {
				task.Arg2 = f
			} else if taskID, ok := arg2.(int); ok {
				task.Arg2 = float64(taskID)
			}

			tasks = append(tasks, task)
			stack = append(stack, taskID)
		}
	}
	return tasks, nil
}

func isNumber(token string) bool {
	_, err := strconv.ParseFloat(token, 64)
	return err == nil
}

func isOperator(token string) bool {
	return token == "+" || token == "-" || token == "*" || token == "/"
}

func precedence(op string) int {
	switch op {
	case "+", "-":
		return 1
	case "*", "/":
		return 2
	}
	return 0
}
