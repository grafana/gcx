# Resources a service uses

A `CloudResource` is a bucket, queue, topic or table that a service's code reads
or writes. Write one for each resource you find, whether or not telemetry knows
it. Then link the Service to it with `READS_FROM` or `WRITES_TO`.

Always link to the `CloudResource`, never to a telemetry entity such as
`S3Bucket`. When telemetry also has the resource, the Knowledge Graph links its
node to the `CloudResource` by itself, by matching `arn`.

## Where to find references

| Source | What it gives |
|---|---|
| Terraform, Pulumi, CloudFormation, Crossplane: `aws_s3_bucket`, `aws_sqs_queue`, `aws_sns_topic`, `aws_dynamodb_table`, `google_storage_bucket`, `google_pubsub_topic`, `KafkaTopic` resources | The resource itself, with its name, account or project, and region |
| IAM policies and roles attached to the service | The access (the actions) and the resource (the ARN) |
| SDK calls in the code | Whether the service reads or writes |
| Environment variables and values in the deploy config | The real name for each environment |

A name in code is often a variable, for example `os.Getenv("ORDERS_QUEUE_URL")`.
Follow it to the deploy config for each environment to find the value.

## Read or write

| Kind | Read (`READS_FROM`) | Write (`WRITES_TO`) |
|---|---|---|
| S3, GCS, Azure Blob | get, list, head, download | put, upload, delete, copy into |
| SQS, Pub/Sub subscription, Service Bus | receive, pull, subscribe | send, publish |
| SNS, Pub/Sub topic, Event Hubs | (subscribers do not read the topic) | publish, send |
| DynamoDB, Firestore, Bigtable | get, query, scan | put, update, delete, batch write |
| Kafka topic | consumer | producer |

A service that does both gets two links. If the code or the policy does not show
the direction, do not guess: list the resource as unresolved.

## Name and properties

The node name is the provider's full id. Telemetry stores AWS ARNs in exactly
this form, so write them the same way.

| Kind | Node name | `kind` | `provider` |
|---|---|---|---|
| S3 bucket | `arn:aws:s3:::<bucket>` | `s3-bucket` | `aws` |
| SQS queue | `arn:aws:sqs:<region>:<account>:<queue>` | `sqs-queue` | `aws` |
| SNS topic | `arn:aws:sns:<region>:<account>:<topic>` | `sns-topic` | `aws` |
| DynamoDB table | `arn:aws:dynamodb:<region>:<account>:table/<table>` | `dynamodb-table` | `aws` |
| GCS bucket | `gs://<bucket>` | `gcs-bucket` | `gcp` |
| Pub/Sub topic | `projects/<project>/topics/<topic>` | `pubsub-topic` | `gcp` |
| Kafka topic | `kafka://<cluster>/<topic>` | `kafka-topic` | `kafka` |
| Azure resource | the Azure resource ID | for example `azure-blob-container` | `azure` |

- An SQS queue URL `https://sqs.<region>.amazonaws.com/<account>/<queue>` gives
  the region, account and queue name of the ARN.
- Use the ARN partition the config shows (`aws`, `aws-cn`, `aws-us-gov`).
- For another kind, use the provider's full id and a short `kind` in the same
  style.

| Property | Value | Required |
|---|---|---|
| `kind` | From the table above | yes |
| `provider` | From the table above | yes |
| `resource_name` | The short name: bucket, queue, table or topic | no |
| `arn` | For AWS kinds, the same value as the node name. Without it, telemetry cannot join | AWS only |
| `account_id`, `region` | AWS account and region, when known. An S3 ARN has neither | no |
| `project_id`, `region` | GCP project and location | no |
| `cluster` | Kafka cluster | no |
| `managed_by` | `gcx-map-source-code` | always set |

## Write only what resolves

Write a resource only when its full id resolves to fixed values for one
environment. List these as unresolved instead, with the file where you saw them:

- names built at runtime, for example one bucket per tenant or per date
- wildcard ARNs in a policy, for example `arn:aws:s3:::orders-*`
- an SQS, SNS or DynamoDB ARN without a known account or region
- a reference whose direction is not clear

Resource ids are metadata and may be written. Never write credentials, access
keys, signed URLs, connection strings, or the contents of objects or messages.
