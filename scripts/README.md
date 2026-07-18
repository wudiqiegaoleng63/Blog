# Automation scripts

Repository automation is grouped by responsibility. Scripts must be non-interactive by default, fail closed, avoid printing credentials, and keep generated data outside Git-tracked paths.

```text
scripts/
├── operations/              # Backup, restore, and recovery drills
│   ├── backup-mysql.sh
│   ├── restore-mysql.sh
│   └── verify-backup.sh
└── security/                # Repository privacy and credential checks
    └── check-privacy.sh
```

## Privacy rules

- Never place real secrets beneath the repository. Use an external `SECRETS_DIR` with mode `0700` and files with mode `0600`.
- Backup output belongs in external encrypted storage. `backups/`, database dumps, private keys, credentials and secret directories are ignored as a safety net, not as the primary control.
- Example values must remain visibly non-production placeholders.
- Run `make privacy-check` before commits and in CI.

## Operations

Use the Make targets instead of calling scripts directly where possible:

```bash
make backup-mysql
make restore-mysql
make verify-backup
```

Required variables and destructive-operation confirmation are documented in `docs/operations-runbook.md`.
