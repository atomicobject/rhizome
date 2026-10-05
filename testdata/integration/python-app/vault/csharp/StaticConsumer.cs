using System.Collections.Generic;
using Polyglot.Todo.Constants;

namespace Polyglot.Todo.Worker;

public class StaticConsumer
{
	public bool Match(string status)
	{
		return status == LegalStatuses.Pcs.Active || LegalStatuses.Pcs.TerminalStates.Contains(status);
	}

	public List<CourtViewTranslationModel.CaseCommentOverflow> Build()
	{
		var item = new CourtViewTranslationModel.CaseCommentOverflow();
		_ = typeof(CourtViewTranslationModel.CaseCommentOverflow);
		_ = nameof(CourtViewTranslationModel.CaseCommentOverflow.Text);
		return new List<CourtViewTranslationModel.CaseCommentOverflow> { item };
	}
}
