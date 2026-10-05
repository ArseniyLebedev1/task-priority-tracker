# TaskFlow - трекер задач с приоритетами

Fullstack-проект по ТЗ №1.

## Стек

- Frontend: React 18, TypeScript, Zustand, Vite
- Backend: Go 1.23, `net/http`
- Авторизация: JWT (HS256)
- Хеширование паролей: bcrypt
- Хранение: JSON-файл `backend/data.json`

## Реализовано

- Регистрация и вход с JWT
- 401 для защищённых endpoint без валидного токена
- Создание, получение списка, получение одной задачи, изменение и удаление
- Изоляция задач по `userId`
- Фильтрация `GET /api/tasks?status=todo|in_progress|done`
- Индикация статуса и приоритета
- Изменение статуса прямо из списка
- Защищённый интерфейс задач
- Zustand stores для auth и задач
- Сохранение JWT в localStorage
- Адаптивный интерфейс

## API

### Auth

- `POST /api/auth/register`
- `POST /api/auth/login`

Тело:

```json
{
  "email": "student@example.com",
  "password": "123456"
}
```

### Tasks

Все endpoint требуют:

```http
Authorization: Bearer <token>
```

- `GET /api/tasks`
- `GET /api/tasks?status=in_progress`
- `GET /api/tasks/{id}`
- `POST /api/tasks`
- `PATCH /api/tasks/{id}`
- `DELETE /api/tasks/{id}`

Пример создания:

```json
{
  "title": "Закончить проект",
  "description": "Проверить frontend и backend",
  "status": "todo",
  "priority": "high",
  "dueDate": "2026-09-10"
}
```

## Запуск backend

```bash
cd backend
go mod tidy
JWT_SECRET="replace-this-secret" go run .
```

Backend будет доступен на `http://localhost:8080`.

Для локальной разработки можно запустить просто `go run .`; тогда используется dev-секрет по умолчанию.

## Запуск frontend

В отдельном терминале:

```bash
cd frontend
npm install
npm run dev
```

Открыть `http://localhost:5173`.

## Проверка 401

```bash
curl -i http://localhost:8080/api/tasks
```

Ответ должен содержать `HTTP/1.1 401 Unauthorized`.

## Структура

```text
task-priority-tracker/
├── backend/
│   ├── go.mod
│   ├── main.go
│   └── .gitignore
├── frontend/
│   ├── src/
│   │   ├── components/
│   │   ├── pages/
│   │   ├── api.ts
│   │   ├── store.ts
│   │   ├── types.ts
│   │   └── styles.css
│   ├── package.json
│   └── vite.config.ts
└── README.md
```

## Почему JSON вместо СУБД

В исходном ТЗ база данных не задана. Для пробного проекта выбран JSON-файл: он не требует отдельной инфраструктуры и сохраняет данные после перезапуска. При необходимости storage-слой можно заменить на PostgreSQL без изменения API и frontend.
