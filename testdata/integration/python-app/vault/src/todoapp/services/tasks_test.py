from todoapp.services.tasks import add_task


def test_add_task_smoke():
    task = add_task("demo", "agent", sync=False)
    assert task["id"] == "demo"
