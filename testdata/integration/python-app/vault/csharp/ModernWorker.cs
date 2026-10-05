namespace Polyglot.Todo.Worker
{
    public class ModernWorker
    {
        public void Run(string title, string[] history, bool includeHistory)
        {
            var payloads = includeHistory
                ? [.. history, title]
                : [];

            foreach (var payload in payloads)
            {
                // [[notes/task-flow]] keep modern C# caller coverage aligned with task flow docs
                Polyglot.Todo.SyncClient.PushUpdates(payload);
            }
        }
    }
}
