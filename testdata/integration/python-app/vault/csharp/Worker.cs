namespace Polyglot.Todo.Worker
{
    public class Worker
    {
        public void Run()
        {
            Polyglot.Todo.SyncClient.PushUpdates("worker-run");
        }
    }
}
