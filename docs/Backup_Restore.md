# Backup and Restore

FastSell data has two parts: PostgreSQL metadata and local filesystem images/exports. Back up both together.

## Backup

Stop writes or run during a quiet period, then back up:

```bash
sudo docker exec fastsell-postgres pg_dump -U fastsell fastsell > fastsell.sql
sudo tar -C /srv/fastsell -czf fastsell-data.tgz data config db
```

Store backups off-host.

## Restore

Install FastSell on the target host, stop the stack, restore files, then restore PostgreSQL:

```bash
sudo docker stop fastsell-api fastsell-web fastsell-system-agent
sudo tar -C /srv/fastsell -xzf fastsell-data.tgz
cat fastsell.sql | sudo docker exec -i fastsell-postgres psql -U fastsell -d fastsell
sudo docker start fastsell-system-agent fastsell-api fastsell-web
```

For a full disaster restore, recreate `/srv/fastsell/config/.env` with the correct database password before starting services.

## Notes

- Keep database and image backups from the same point in time when possible.
- Test restore procedures before relying on them.
- Protect backups because they may contain private photos, item notes, and provider configuration.
