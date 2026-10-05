"""Worker that calls sync for functionUse anchor coverage."""

from todoapp.services.sync import push_updates


def run_sync() -> None:
    push_updates({"id": "demo", "title": "worker"})


if __name__ == "__main__":
    run_sync()
