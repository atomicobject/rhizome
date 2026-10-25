use super::*;

#[test]
fn every_library_save_advances_the_revision_shells_receive() {
    let directory = tempfile::tempdir().unwrap();
    let desktop = Desktop::new(directory.path().into());
    let mut library = Library::default();
    assert_eq!(desktop.snapshot(&library)["revision"], 0);
    assert_eq!(desktop.store(&library).unwrap()["revision"], 1);
    library.global_executable = Some("/bin/rzm".into());
    let saved = desktop.store(&library).unwrap();
    assert_eq!(saved["revision"], 2);
    assert_eq!(saved["globalExecutable"], "/bin/rzm");
    assert_eq!(desktop.snapshot(&library), saved);
}
#[test]
fn setup_and_scope_actions_decode_the_shell_contract() {
    let choices = json!({"workflow":"agentic-engineering", "addons":["action-items"], "agents":["codex"], "search":"off", "skip":["dist"], "keepIndexed":["testdata"], "includeIgnored":["nested"]});
    for operation in ["setup-report", "initialize", "scope", "scope-edit"] {
        let mut input = json!({"operation":operation, "id":"repo", "worktree":"/worktree"});
        if operation == "setup-report" || operation == "initialize" {
            input["choices"] = choices.clone();
        }
        if operation == "initialize" {
            input["key"] = "synthetic-search-secret".into();
        }
        if operation == "scope-edit" {
            input["edits"] =
                json!({"skip":["dist"], "removeRules":["/data/"], "includeIgnored":["nested"]});
        }
        let action: Action = serde_json::from_value(input.clone()).unwrap();
        let request = match &action {
            Action::SetupReport { choices, .. } => Request {
                operation: "setup-report",
                setup: choices.as_ref(),
                ..Request::default()
            },
            Action::Initialize { choices, key, .. } => Request {
                operation: "initialize",
                setup: Some(choices),
                key: key.as_ref(),
                ..Request::default()
            },
            Action::Scope { .. } => Request {
                operation: "scope",
                ..Request::default()
            },
            Action::ScopeEdit { edits, .. } => Request {
                operation: "scope-edit",
                edits: Some(edits),
                ..Request::default()
            },
            _ => panic!("unexpected action"),
        };
        let payload = serde_json::to_value(request).unwrap();
        assert_eq!(payload["operation"], operation);
        if operation == "setup-report" || operation == "initialize" {
            assert_eq!(payload["setup"], choices);
        }
        if operation == "initialize" {
            assert_eq!(payload["key"], "synthetic-search-secret");
        }
        if operation == "scope-edit" {
            assert_eq!(payload["edits"], input["edits"]);
        }
        input["surprise"] = true.into();
        assert!(serde_json::from_value::<Action>(input).is_err());
    }
    assert!(matches!(
        serde_json::from_value::<Action>(
            json!({"operation":"setup-report", "id":"repo", "worktree":"/worktree"})
        )
        .unwrap(),
        Action::SetupReport { choices: None, .. }
    ));
    for input in [
        json!({"operation":"setup-report", "id":"repo", "worktree":"/worktree", "key":"synthetic-search-secret"}),
        json!({"operation":"initialize", "id":"repo", "worktree":"/worktree"}),
        json!({"operation":"scope-edit", "id":"repo", "worktree":"/worktree", "edits":{"skip":[], "remove_rules":[], "includeIgnored":[]}}),
        json!({"operation":"initialize", "id":"repo", "worktree":"/worktree", "choices":{"workflow":"", "addons":[], "agents":[], "search":"", "skip":[], "keep_indexed":[], "includeIgnored":[]}}),
        json!({"operation":"scope", "id":"repo", "worktree":"/worktree", "key":"synthetic-search-secret"}),
    ] {
        assert!(serde_json::from_value::<Action>(input).is_err());
    }
    let mut unknown_choices = choices;
    unknown_choices["surprise"] = true.into();
    assert!(serde_json::from_value::<Action>(json!({"operation":"setup-report", "id":"repo", "worktree":"/worktree", "choices":unknown_choices})).is_err());
}

#[test]
fn page_requests_carry_the_command_beside_the_operation() {
    let action: Action = serde_json::from_value(
        json!({"operation": "page", "command": "section", "section": "agent"}),
    )
    .unwrap();
    assert!(matches!(
        action,
        Action::Page(crate::page::Command::Section { .. })
    ));
    assert!(serde_json::from_value::<Action>(
        json!({"operation": "page", "command": "section", "section": "x"})
    )
    .is_err());
}

#[test]
fn setup_waits_for_import_to_release_user_configuration() {
    tauri::async_runtime::block_on(async {
        let directory = tempfile::tempdir().unwrap();
        let desktop = Desktop::new(directory.path().into());
        let importing = desktop.global.lock().await;
        let request = Request {
            operation: "initialize",
            ..Request::default()
        };
        let setup = worktree_action(&desktop, "missing-repository", "/worktree", request);
        tokio::pin!(setup);
        assert!(
            tokio::time::timeout(std::time::Duration::from_millis(30), &mut setup)
                .await
                .is_err(),
            "setup must not reach repository execution while an import owns user configuration"
        );
        drop(importing);
        let result = tokio::time::timeout(std::time::Duration::from_secs(1), setup)
            .await
            .expect("setup should resume after import finishes");
        assert!(
            result.is_err(),
            "the nonexistent repository must remain rejected"
        );
    });
}
