# Финальный проект 1 семестра

REST API сервис для загрузки и выгрузки данных о ценах.

## Требования

- Go 1.23+
- PostgreSQL 12+

## Установка

1. Установите Go и PostgreSQL если еще не установлены

2. Создайте пользователя и базу данных в PostgreSQL:

```bash
sudo -u postgres psql
CREATE USER validator WITH PASSWORD 'val1dat0r';
CREATE DATABASE "project-sem-1" OWNER validator;
\q
```

3. Запустите скрипт подготовки:

```bash
chmod +x scripts/prepare.sh
./scripts/prepare.sh
```

## Запуск

```bash
chmod +x scripts/run.sh
./scripts/run.sh
```

Или просто:

```bash
go run main.go
```

Сервер запустится на http://localhost:8080

## Тестирование

Запустите тесты:

```bash
chmod +x scripts/tests.sh
./scripts/tests.sh 1
```

Уровни тестирования: 1 (простой), 2 (продвинутый), 3 (сложный)

## API

POST /api/v0/prices - загрузка данных из ZIP архива с CSV
GET /api/v0/prices - выгрузка всех данных в ZIP архиве с CSV

Пример загрузки:

```bash
curl -X POST -F "file=@sample_data.zip" http://localhost:8080/api/v0/prices
```

Пример выгрузки:

```bash
curl -X GET http://localhost:8080/api/v0/prices -o response.zip
```

## Формат данных

CSV файл должен содержать колонки: id, name, category, price, create_date

Пример:
```csv
id,name,category,price,create_date
1,Товар 1,Категория 1,100.50,2024-01-01
2,Товар 2,Категория 2,200.75,2024-01-02
```
