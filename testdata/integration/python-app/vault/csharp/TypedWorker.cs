using Polyglot.Todo;

namespace Polyglot.Todo.Worker
{
    public class SyncGateway
    {
        public void Push(string payload)
        {
            SyncClient.PushUpdates(payload);
        }
    }

    public class TypedWorker
    {
        private readonly SyncGateway _gateway = new();

        public void Run()
        {
            SyncGateway local = new();
            _gateway.Push("field");
            local.Push("local");
        }
    }
}
