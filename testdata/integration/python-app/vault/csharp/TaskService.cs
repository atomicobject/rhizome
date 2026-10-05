using System;

namespace Polyglot.Todo
{
    public class TaskService
    {
        public void AddTask(string title)
        {
            // [[notes/task-flow]] keep csharp task intake aligned with docs
            // [[notes/release-plan]] confirm rollout milestones
            SyncClient.PushUpdates(title);
        }
    }
}
