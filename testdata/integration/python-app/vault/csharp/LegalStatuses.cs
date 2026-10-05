using System.Collections.Generic;

namespace Polyglot.Todo.Constants;

public static class LegalStatuses
{
	public static class Pcs
	{
		public const string Active = "active";
		public static readonly HashSet<string> TerminalStates = new();
	}
}
