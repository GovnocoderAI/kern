# Kern — быстрый агент для терминала

**Kern** — open-source агент для программирования прямо в терминале. Написан на Go, собирается в один статический бинарник и совместим с конфигами `opencode`.

## Статус

Сейчас идёт разработка **v0.1**. Ядро (провайдеры, стриминг, тулзы, разрешения, консольный интерфейс, сессии) уже работает end-to-end. Полноэкранный TUI, MCP, субагенты, LSP и веб-интерфейс — в [роадмапе](../ROADMAP.md).

## Установка из исходников

Нужен Go 1.23+.

```bash
git clone https://github.com/kern-ai/kern.git
cd kern
go build -ldflags "-s -w" -o bin/kern ./cmd/kern
```

Linux/macOS:

```bash
./bin/kern
```

Windows (PowerShell):

```powershell
.\bin\kern.exe
```

## Быстрый старт

```bash
kern                      # интерактивная сессия в текущем проекте
kern ~/code/my-project    # стартовать в другом проекте
kern run "объясни этот репозиторий"
kern models               # список моделей
kern doctor               # диагностика конфига, провайдеров и ключей
```

## Подключение твоих ключей

Kern читает твой текущий `opencode.json` без миграции. Ключи можно хранить:

1. в конфиге провайдера (`apiKey`);
2. в `~/.config/kern/auth.json` через `kern auth set`;
3. в `~/.local/share/opencode/auth.json` (подхватывается автоматически);
4. в переменных окружения (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `<PROVIDER>_API_KEY`).

```bash
kern auth set plusvibe sk-...
kern auth list
kern doctor
```

Пример оверлея `kern.json` в проекте:

```jsonc
{
  "model": "plusvibe/claude-opus-5.5",
  "permissions": [
    { "action": "shell", "resource": "git push *", "effect": "ask" },
    { "action": "shell", "resource": "rm -rf *", "effect": "deny" }
  ]
}
```

## Команды внутри сессии

```
/help              помощь
/model [ref]       показать или сменить модель (provider/model)
/models            список моделей
/tools             список тулзов
/session           текущая сессия
/cost              расход токенов
/clear             очистить диалог
/exit              выход
```

## Безопасность

- Телеметрии нет.
- Ключи не выводятся в `config show`, файл `auth.json` создаётся с правами `0600`.
- Тулзы не выходят за пределы рабочей директории.
- Опасные действия (запись, shell, webfetch) по умолчанию требуют подтверждения; `--auto` снимает подтверждения, кроме явных `deny`.

## Лицензия

Apache-2.0.
