#!/bin/bash

set -e

cd "$(dirname "$0")/.."

echo "Установка зависимостей..."
go mod download
go mod tidy

export PGPASSWORD="val1dat0r"

echo "Ожидание PostgreSQL..."
until psql -h localhost -p 5432 -U validator -d postgres -c '\q' 2>/dev/null; do
    echo "Ждем PostgreSQL..."
    sleep 1
done

echo "Создание базы данных..."
psql -h localhost -p 5432 -U validator -d postgres -c "SELECT 1 FROM pg_database WHERE datname = 'project-sem-1'" | grep -q 1 || \
    psql -h localhost -p 5432 -U validator -d postgres -c "CREATE DATABASE \"project-sem-1\""

echo "Готово!"
