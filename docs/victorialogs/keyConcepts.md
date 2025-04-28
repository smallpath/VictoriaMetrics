---
weight: 2
title: Key concepts
menu:
  docs:
    identifier: vl-key-concepts
    parent: victorialogs
    weight: 2
    title: Key concepts
tags:
  - logs
aliases:
- /victorialogs/keyConcepts.html
---
## Data model

[VictoriaLogs](https://docs.victoriametrics.com/victorialogs/) handles both structured and unstructured logs.
Each log entry needs at least one [log message field](#message-field). You can add any number of extra `key=value` fields to your logs.
A log entry looks like a simple [JSON](https://www.json.org/json-en.html) object with string keys and string values.
For example:

```json
{
  "job": "my-app",
  "instance": "host123:4567",
  "level": "error",
  "client_ip": "1.2.3.4",
  "trace_id": "1234-56789-abcdef",
  "_msg": "failed to serve the client request"
}
```

Empty values are the same as missing values. These log entries are all the same because they only have one matching non-empty field - [`_msg`](#message-field):

```json
{
  "_msg": "foo bar",
  "some_field": "",
  "another_field": ""
}
```

```json
{
  "_msg": "foo bar",
  "third_field": "",
}
```

```json
{
  "_msg": "foo bar",
}
```

VictoriaLogs changes nested JSON into flat JSON when [ingesting data](https://docs.victoriametrics.com/victorialogs/data-ingestion/). It follows these rules:

- Nested objects are flattened by joining keys with a dot (`.`). For example:

  ```json
  {
    "host": {
      "name": "foobar"
      "os": {
        "version": "1.2.3"
      }
    }
  }
  ```

  becomes:

  ```json
  {
    "host.name": "foobar",
    "host.os.version": "1.2.3"
  }
  ```

- Arrays, numbers and booleans are turned into strings. This makes [full-text search](https://docs.victoriametrics.com/victorialogs/logsql/) easier.
  For example:

  ```json
  {
    "tags": ["foo", "bar"],
    "offset": 12345,
    "is_error": false
  }
  ```

  becomes:

  ```json
  {
    "tags": "[\"foo\", \"bar\"]",
    "offset": "12345",
    "is_error": "false"
  }
  ```

Field names and values can contain any characters. These characters must be properly encoded 
during [data ingestion](https://docs.victoriametrics.com/victorialogs/data-ingestion/)
using [JSON string encoding](https://www.rfc-editor.org/rfc/rfc7159.html#section-7).
Non-English characters must use [UTF-8](https://en.wikipedia.org/wiki/UTF-8) encoding:

```json
{
  "field with whitespace": "value\nwith\nnewlines",
  "Поле": "价值"
}
```

VictoriaLogs indexes all fields in all [ingested](https://docs.victoriametrics.com/victorialogs/data-ingestion/) logs automatically.
This lets you do [full-text search](https://docs.victoriametrics.com/victorialogs/logsql/) across all fields.

VictoriaLogs has these special fields in addition to any [other fields](#other-fields) you add:

* [`_msg` field](#message-field)
* [`_time` field](#time-field)
* [`_stream` and `_stream_id` fields](#stream-fields)

### Message field

Every [log entry](#data-model) must have at least a `_msg` field with the actual log message. The simplest
log entry looks like this:

```json
{
  "_msg": "some log message"
}
```

If your log message uses a different field name than `_msg`, you can tell VictoriaLogs about it using the `_msg_field` HTTP query parameter or the `VL-Msg-Field` HTTP header
when [sending data](https://docs.victoriametrics.com/victorialogs/data-ingestion/).
For example, if your log message is in the `event.original` field, use `_msg_field=event.original`.
See [these docs](https://docs.victoriametrics.com/victorialogs/data-ingestion/#http-parameters) for more details.

If the `_msg` field is empty after trying to get it from the field you specified with `_msg_field`, VictoriaLogs will use the value you set with the `-defaultMsgValue` command-line flag.

### Time field

Your [log entries](#data-model) can include a `_time` field that shows when the log entry was created.
This field must use one of these formats:

- [ISO8601](https://en.wikipedia.org/wiki/ISO_8601) or [RFC3339](https://www.rfc-editor.org/rfc/rfc3339).
  Examples: `2023-06-20T15:32:10Z` or `2023-06-20 15:32:10.123456789+02:00`.
  If you don't include timezone info (like `2023-06-20 15:32:10`),
  VictoriaLogs will use the local timezone of the server it's running on.

- Unix timestamp in seconds, milliseconds, microseconds or nanoseconds. Examples: `1686026893` (seconds), `1686026893735` (milliseconds),
  `1686026893735321` (microseconds) or `1686026893735321098` (nanoseconds).

Here's an example [log entry](#data-model) with a timestamp (millisecond precision) in the `_time` field:

```json
{
  "_msg": "some log message",
  "_time": "2023-04-12T06:38:11.095Z"
}
```

If your timestamp uses a different field name than `_time`, you can specify which field has the timestamp
using the `_time_field` HTTP query parameter or the `VL-Time-Field` HTTP header when [sending data](https://docs.victoriametrics.com/victorialogs/data-ingestion/).
For example, if your timestamp is in the `event.created` field, use `_time_field=event.created`.
See [these docs](https://docs.victoriametrics.com/victorialogs/data-ingestion/#http-parameters) for more details.

If the `_time` field is missing or equals `0`, VictoriaLogs will use the time when it received the log as the timestamp.

The `_time` field helps the [time filter](https://docs.victoriametrics.com/victorialogs/logsql/#time-filter) quickly narrow down
searches to a specific time range.

### Stream fields

Some [log fields](#data-model) can identify which application instance created the logs.
This could be a single field like `instance="host123:456"` or multiple fields like
`{datacenter="...", env="...", job="...", instance="..."}` or
`{kubernetes.namespace="...", kubernetes.node.name="...", kubernetes.pod.name="...", kubernetes.container.name="..."}`.

Logs from a single application instance form a **log stream** in VictoriaLogs.
VictoriaLogs stores and [searches](https://docs.victoriametrics.com/victorialogs/logsql/#stream-filter) individual log streams efficiently.
This gives you:

- Less disk space usage, because logs from one application compress better
  than mixed logs from many different applications.

- Faster searches, because VictoriaLogs scans less data
  when [searching by stream fields](https://docs.victoriametrics.com/victorialogs/logsql/#stream-filter).

Every log entry belongs to a log stream. Each log stream has these special fields:

- `_stream_id` - a unique ID for the log stream. You can find all logs for a specific stream using
  the [`_stream_id:...` filter](https://docs.victoriametrics.com/victorialogs/logsql/#_stream_id-filter).

- `_stream` - contains stream labels in a format similar to [Prometheus metric labels](https://docs.victoriametrics.com/keyconcepts/#labels):
  ```
  {field1="value1", ..., fieldN="valueN"}
  ```
  For example, if the `host` and `app` fields define your stream, then the `_stream` field will show `{host="host-123",app="my-app"}`
  for logs with `host="host-123"` and `app="my-app"`. You can search the `_stream` field
  using [stream filters](https://docs.victoriametrics.com/victorialogs/logsql/#stream-filter).

By default, the `_stream` field is empty (`{}`), because VictoriaLogs can't automatically tell
which fields identify each log stream. This might not give you the best performance.
You should specify which fields define your streams using the `_stream_fields` query parameter
when [sending data](https://docs.victoriametrics.com/victorialogs/data-ingestion/).
For example, if your Kubernetes container logs look like this:

```json
{
  "kubernetes.namespace": "some-namespace",
  "kubernetes.node.name": "some-node",
  "kubernetes.pod.name": "some-pod",
  "kubernetes.container.name": "some-container",
  "_msg": "some log message"
}
```

use `_stream_fields=kubernetes.namespace,kubernetes.node.name,kubernetes.pod.name,kubernetes.container.name`
when [sending data](https://docs.victoriametrics.com/victorialogs/data-ingestion/) to properly store
each container's logs in separate streams.

#### How to choose which fields to use for log streams?

[Log streams](#stream-fields) should include [fields](#data-model) that uniquely identify which application instance generated the logs.
For example, `container`, `instance` and `host` are good choices for stream fields.

You can add more fields to log streams if they **stay the same throughout the application instance's lifetime**.
For example, `namespace`, `node`, `pod` and `job` are good additional stream fields. Adding these fields to streams
makes sense if you plan to search using them and want faster searching with [stream filters](https://docs.victoriametrics.com/victorialogs/logsql/#stream-filter).

You **don't need to add all constant fields to log streams**, as this might use more resources during data ingestion and searching.

**Never add changing fields to streams if these fields change with each log entry from the same source**.
For example, `ip`, `user_id` and `trace_id` **should never be added to log streams**, as this can cause [high cardinality problems](#high-cardinality).

#### High cardinality

Some fields in your [logs](#data-model) might contain many unique values across different log entries.
For example, fields like `ip`, `user_id` or `trace_id` often have many different values.
VictoriaLogs handles these fields fine unless you make them part of your [log streams](#stream-fields).

**Never** add high-cardinality fields to [log streams](#stream-fields), as this can cause:

- Slower performance during [data ingestion](https://docs.victoriametrics.com/victorialogs/data-ingestion/)
  and [querying](https://docs.victoriametrics.com/victorialogs/querying/)
- Higher memory usage
- Higher CPU usage
- More disk space usage
- More disk read/write operations

VictoriaLogs provides a `vl_streams_created_total` [metric](https://docs.victoriametrics.com/victorialogs/#monitoring)
that shows how many streams were created since the last restart. If this number grows quickly
over a long time, you might have high cardinality issues.
You can make VictoriaLogs log all newly created streams by using the `-logNewStreams` command-line flag.
This helps find and remove high-cardinality fields from your [log streams](#stream-fields).

### Other fields

Each log entry can have any number of [fields](#data-model) in addition to [`_msg`](#message-field) and [`_time`](#time-field).
Examples include `level`, `ip`, `user_id`, `trace_id`, etc. These fields make [search queries](https://docs.victoriametrics.com/victorialogs/logsql/) simpler and faster.
Searching a specific `trace_id` field is usually faster than searching for that ID inside a long [log message](#message-field).
For example, `trace_id:="XXXX-YYYY-ZZZZ"` is faster than `_msg:"trace_id=XXXX-YYYY-ZZZZ"`.

See [LogsQL docs](https://docs.victoriametrics.com/victorialogs/logsql/) for more details.
