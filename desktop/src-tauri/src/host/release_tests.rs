use super::*;
use std::{
    io::{BufRead, BufReader, Read, Write},
    net::TcpListener,
    sync::mpsc,
};
use url::Url;

/// A fake runtime answering one JSON body per connection, in order, and
/// recording each request line.
fn runtime(bodies: &[&str]) -> (Api, mpsc::Receiver<String>) {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let origin = Url::parse(&format!("http://{}/", listener.local_addr().unwrap())).unwrap();
    let (sent, received) = mpsc::channel();
    let bodies: Vec<String> = bodies.iter().map(|b| b.to_string()).collect();
    std::thread::spawn(move || {
        for body in bodies {
            let (mut stream, _) = listener.accept().unwrap();
            let mut reader = BufReader::new(stream.try_clone().unwrap());
            let mut line = String::new();
            reader.read_line(&mut line).unwrap();
            let mut length = 0;
            loop {
                let mut header = String::new();
                reader.read_line(&mut header).unwrap();
                if header.trim().is_empty() {
                    break;
                }
                if let Some(v) = header.to_ascii_lowercase().strip_prefix("content-length:") {
                    length = v.trim().parse().unwrap();
                }
            }
            reader.read_exact(&mut vec![0; length]).unwrap();
            let _ = sent.send(line.trim().to_string());
            let reply = format!(
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                body.len()
            );
            stream.write_all(reply.as_bytes()).unwrap();
        }
    });
    (Api::new(origin).unwrap(), received)
}

fn block_on<F: std::future::Future>(future: F) -> F::Output {
    tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .unwrap()
        .block_on(future)
}

const IDLE: &str = r#"{"running":null,"dispatching":null}"#;
const RUNNING: &str = r#"{"running":{"task":"digest","name":"Daily digest","runId":"r1"}}"#;
const DISPATCHING: &str =
    r#"{"running":null,"dispatching":{"pid":42,"startedAt":"2026-11-22T12:00:00Z"}}"#;

#[test]
fn the_drain_answer_decides_which_runtimes_are_busy_and_stop_reaches_each() {
    // Status streamed nothing running; the drain answers are what count.
    let (notes, notes_seen) = runtime(&[RUNNING, r#"{"stopped":true}"#]);
    let (site, site_seen) = runtime(&[DISPATCHING, r#"{"stopped":true}"#]);
    let (idle, idle_seen) = runtime(&[IDLE]);
    let busy = block_on(drain(vec![
        ("notes".into(), notes),
        ("site".into(), site),
        ("idle".into(), idle),
    ]));
    assert_eq!(
        Busy::describe(&busy),
        [
            ("notes".to_string(), "Daily digest".to_string()),
            ("site".to_string(), "Scheduled tasks".to_string())
        ]
    );
    block_on(stop_runs(&busy));
    for seen in [&notes_seen, &site_seen] {
        assert_eq!(seen.recv().unwrap(), "POST /api/tasks/host/drain HTTP/1.1");
        assert_eq!(seen.recv().unwrap(), "POST /api/tasks/stop HTTP/1.1");
    }
    assert_eq!(
        idle_seen.recv().unwrap(),
        "POST /api/tasks/host/drain HTTP/1.1"
    );
    assert!(
        idle_seen.try_recv().is_err(),
        "an idle runtime is not stopped"
    );
}

#[test]
fn a_runtime_that_does_not_answer_the_drain_is_still_stopped() {
    // Nothing listens on the origin, so the drain fails.
    let gone = Api::new(Url::parse("http://127.0.0.1:9/").unwrap()).unwrap();
    let busy = block_on(drain(vec![("gone".into(), gone)]));
    assert_eq!(busy.len(), 1);
    assert!(
        Busy::describe(&busy).is_empty(),
        "no run to name in the question"
    );
}

#[test]
fn waiting_lasts_until_the_drain_reports_no_work() {
    let (api, seen) = runtime(&[RUNNING, RUNNING, IDLE]);
    let busy = block_on(drain(vec![("notes".into(), api)]));
    block_on(wait_idle(&busy));
    assert_eq!(seen.iter().take(3).count(), 3, "drained until idle");
}

#[test]
fn waiting_does_not_treat_a_failed_answer_as_idle() {
    // The runtime answers once that a run is going, then stops answering.
    let (api, seen) = runtime(&[RUNNING]);
    let busy = block_on(drain(vec![("notes".into(), api)]));
    let started = std::time::Instant::now();
    block_on(wait_idle_up_to(&busy, 3));
    assert!(
        started.elapsed() >= POLL * 2,
        "kept asking after failed answers instead of quitting at once"
    );
    assert_eq!(seen.iter().count(), 1, "only the first drain was answered");
}
