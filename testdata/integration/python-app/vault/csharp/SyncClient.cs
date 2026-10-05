using System;

namespace Polyglot.Todo
{
    [Instrument(Tag = "sync")]
    public static class SyncClient
    {
        public static void PushUpdates(string payload)
        {
            // [[notes/sync-strategy]] keep the C# sync client behavior in sync with the shared docs
            Console.WriteLine($"pushing {payload}");
        }
    }

    [AttributeUsage(AttributeTargets.Method | AttributeTargets.Class)]
    public class InstrumentAttribute : Attribute
    {
        public string Tag { get; set; }
    }
}
