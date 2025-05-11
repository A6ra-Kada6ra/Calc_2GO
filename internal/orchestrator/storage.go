package orchestrator

import (
	"Calc_2GO/models"
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Storage struct {
	db *sql.DB
}

func NewStorage() *Storage {
	db, err := sql.Open("sqlite3", "./calc.db?_foreign_keys=on")
	if err != nil {
		log.Fatal(err)
	}

	if err := createTables(db); err != nil {
		log.Fatal(err)
	}

	return &Storage{db: db}
}

func createTables(db *sql.DB) error {
	_, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS users (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            login TEXT UNIQUE NOT NULL,
            password_hash TEXT NOT NULL
        );
        
        CREATE TABLE IF NOT EXISTS expressions (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            user_id INTEGER NOT NULL,
            expression TEXT NOT NULL,
            status TEXT NOT NULL,
            result REAL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (user_id) REFERENCES users(id)
        );
        
        CREATE TABLE IF NOT EXISTS tasks (
            id INTEGER NOT NULL,
            user_id INTEGER NOT NULL,
            arg1 REAL NOT NULL,
            arg2 REAL NOT NULL,
            operation TEXT NOT NULL,
            operation_time INTEGER NOT NULL,
            status TEXT DEFAULT 'pending',
            expression_id INTEGER NOT NULL,
            result REAL,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (user_id) REFERENCES users(id),
            FOREIGN KEY (expression_id) REFERENCES expressions(id)
        );
        
        CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
        CREATE INDEX IF NOT EXISTS idx_expressions_user ON expressions(user_id);
    `)
	return err
}

func (s *Storage) CreateUser(login, passwordHash string) (int, error) {
	res, err := s.db.Exec(
		"INSERT INTO users (login, password_hash) VALUES (?, ?)",
		login, passwordHash,
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return int(id), nil
}

func (s *Storage) GetUserByLogin(login string) (*models.User, error) {
	var user models.User
	err := s.db.QueryRow(
		"SELECT id, login, password_hash FROM users WHERE login = ?",
		login,
	).Scan(&user.ID, &user.Login, &user.PasswordHash)

	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *Storage) AddExpression(userID int, expr string) (int, error) {
	res, err := s.db.Exec(
		"INSERT INTO expressions (user_id, expression, status) VALUES (?, ?, ?)",
		userID, expr, "pending",
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return int(id), nil
}

func (s *Storage) GetExpressions(userID int) ([]*models.Expression, error) {
	rows, err := s.db.Query(
		`SELECT id, user_id, expression, status, 
         COALESCE(result, 0) as result 
         FROM expressions WHERE user_id = ?`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var expressions []*models.Expression
	for rows.Next() {
		var expr models.Expression
		err := rows.Scan(&expr.ID, &expr.UserID, &expr.Expression, &expr.Status, &expr.Result)
		if err != nil {
			return nil, err
		}
		expressions = append(expressions, &expr)
	}
	return expressions, nil
}

func (s *Storage) GetExpression(id int) (*models.Expression, error) {
	var expr models.Expression
	err := s.db.QueryRow(
		"SELECT id, user_id, expression, status, result FROM expressions WHERE id = ?",
		id,
	).Scan(&expr.ID, &expr.UserID, &expr.Expression, &expr.Status, &expr.Result)

	if err != nil {
		return nil, err
	}
	return &expr, nil
}

func (s *Storage) AddTasks(tasks []models.Task) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, task := range tasks {
		var exists bool
		err := tx.QueryRow(
			"SELECT 1 FROM expressions WHERE id = ?",
			task.ID/1000,
		).Scan(&exists)

		if err != nil {
			return fmt.Errorf("выражение %d не найдено: %v", task.ID/1000, err)
		}

		_, err = tx.Exec(
			"INSERT INTO tasks (id, user_id, arg1, arg2, operation, operation_time, status, expression_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			task.ID,
			task.UserID,
			task.Arg1,
			task.Arg2,
			task.Operation,
			task.OperationTime.Milliseconds(),
			"pending",
			task.ID/1000,
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Storage) GetNextTask() (*models.Task, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var task models.Task
	var opTimeMs int64

	err = tx.QueryRow(`
        SELECT id, user_id, arg1, arg2, operation, operation_time 
        FROM tasks 
        WHERE status = 'pending'
        ORDER BY id LIMIT 1`).Scan(
		&task.ID,
		&task.UserID,
		&task.Arg1,
		&task.Arg2,
		&task.Operation,
		&opTimeMs,
	)

	if err != nil {
		return nil, err
	}
	task.OperationTime = time.Duration(opTimeMs) * time.Millisecond

	// Обновляем статус задачи
	_, err = tx.Exec(`
        UPDATE tasks SET status = 'in_progress' 
        WHERE id = ?`, task.ID)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return &task, nil
}

func (s *Storage) UpdateExpressionStatus(id int, status string, result float64) error {
	_, err := s.db.Exec(
		"UPDATE expressions SET status = ?, result = ? WHERE id = ?",
		status, result, id,
	)
	return err
}
func (s *Storage) GetTasksByExpressionID(exprID int) ([]models.Task, error) {
	rows, err := s.db.Query(`
        SELECT id, status 
        FROM tasks 
        WHERE expression_id = ?`,
		exprID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []models.Task
	for rows.Next() {
		var task models.Task
		if err := rows.Scan(&task.ID, &task.Status); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func (s *Storage) UpdateTaskStatus(taskID int, status string) error {
	_, err := s.db.Exec(
		"UPDATE tasks SET status = ? WHERE id = ?",
		status, taskID,
	)
	return err
}
func (s *Storage) GetTaskResult(taskID int) (float64, error) {
	var result sql.NullFloat64
	err := s.db.QueryRow(
		"SELECT result FROM tasks WHERE id = ?",
		taskID,
	).Scan(&result)

	if err != nil {
		return 0, fmt.Errorf("task %d not found: %v", taskID, err)
	}
	if !result.Valid {
		return 0, fmt.Errorf("result for task %d not ready", taskID)
	}
	return result.Float64, nil
}

func (s *Storage) SaveTaskResult(taskID int, result float64) error {
	_, err := s.db.Exec(
		"UPDATE tasks SET result = ?, status = 'done' WHERE id = ?",
		result, taskID,
	)
	return err
}
