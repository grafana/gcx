# Resources a service uses

A `CloudResource` is a bucket, queue, topic, table or database that a
service's code reads or writes. A Pub/Sub subscription counts as a queue here: a service that pulls
from one reads that subscription, not its topic. Write one for each resource you
find, whether or not telemetry knows it. Then link the Service to it with `READS_FROM` or `WRITES_TO`.

Always link to the `CloudResource`, never to a telemetry entity such as
`S3Bucket`. When telemetry also has an S3 bucket, SQS queue or DynamoDB table,
the Knowledge Graph links its node to the `CloudResource` by itself, by matching
`arn`. Other kinds, such as topics and GCS buckets, are written the same way but
are not linked to telemetry yet.

## Where to find references

| Source | What it gives |
|---|---|
| Terraform, Pulumi, CloudFormation, Crossplane: `aws_s3_bucket`, `aws_sqs_queue`, `aws_sns_topic`, `aws_dynamodb_table`, `google_storage_bucket`, `google_pubsub_topic`, `google_pubsub_subscription`, `azurerm_storage_container`, `azurerm_servicebus_queue`, `azurerm_servicebus_topic`, `azurerm_servicebus_subscription`, `aws_db_instance`, `aws_rds_cluster`, `google_sql_database_instance`, `azurerm_mssql_database`, `KafkaTopic` resources | The resource itself, with its name, account or project, and region |
| IAM policies and roles attached to the service, Azure role assignments | The access (the actions or role) and the resource (the ARN or Azure resource ID) |
| SDK calls in the code | Whether the service reads or writes |
| Environment variables and values in the deploy config | The real name for each environment |

A name in code is often a variable, for example `os.Getenv("ORDERS_QUEUE_URL")`.
Follow it to the deploy config for each environment to find the value.

## Read or write

| Kind | Read (`READS_FROM`) | Write (`WRITES_TO`) |
|---|---|---|
| S3 bucket, GCS bucket, Azure blob container | get, list, head, download | put, upload, delete, copy into |
| SQS queue, Azure Service Bus queue | receive | send |
| SNS topic, Pub/Sub topic, Azure Service Bus topic | (a subscriber reads its own queue or subscription, not the topic) | publish |
| Pub/Sub subscription, Azure Service Bus subscription | pull, streaming pull, receive | (none: publishers write to the topic) |
| DynamoDB table | get, query, scan | put, update, delete, batch write |
| Kafka topic | consumer | producer |
| Database instance or cluster (RDS, Aurora, Cloud SQL, Azure SQL) | `SELECT` and other read queries | `INSERT`, `UPDATE`, `DELETE`, schema migrations |

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
| Pub/Sub subscription | `projects/<project>/subscriptions/<subscription>` | `pubsub-subscription` | `gcp` |
| Kafka topic | `kafka://<cluster>/<topic>` | `kafka-topic` | `kafka` |
| RDS DB instance | `arn:aws:rds:<region>:<account>:db:<identifier>` | `rds-db-instance` | `aws` |
| RDS or Aurora DB cluster | `arn:aws:rds:<region>:<account>:cluster:<identifier>` | `rds-db-cluster` | `aws` |
| Cloud SQL instance | `projects/<project>/instances/<instance>` | `cloudsql-instance` | `gcp` |
| Azure SQL database | `/subscriptions/<subscription>/resourceGroups/<group>/providers/Microsoft.Sql/servers/<server>/databases/<database>` | `azure-sql-database` | `azure` |
| Azure blob container | `/subscriptions/<subscription>/resourceGroups/<group>/providers/Microsoft.Storage/storageAccounts/<account>/blobServices/default/containers/<container>` | `azure-blob-container` | `azure` |
| Azure Service Bus queue | `/subscriptions/<subscription>/resourceGroups/<group>/providers/Microsoft.ServiceBus/namespaces/<namespace>/queues/<queue>` | `azure-servicebus-queue` | `azure` |
| Azure Service Bus topic | `/subscriptions/<subscription>/resourceGroups/<group>/providers/Microsoft.ServiceBus/namespaces/<namespace>/topics/<topic>` | `azure-servicebus-topic` | `azure` |
| Azure Service Bus subscription | the topic's ID followed by `/subscriptions/<subscription-name>` | `azure-servicebus-subscription` | `azure` |

- An Azure resource ID needs the subscription and resource group. Code and
  connection strings usually give only the storage account or namespace. Take
  the rest from the infrastructure code or deploy config. If it is not there,
  list the resource as unresolved. Never copy a connection string or key.
- A Service Bus subscription counts as a queue, like a Pub/Sub subscription: a
  service that receives from it reads the subscription, not its topic.

- An SQS queue URL `https://sqs.<region>.amazonaws.com/<account>/<queue>` gives
  the region, account and queue name of the ARN.
- An RDS endpoint `<identifier>.<id>.<region>.rds.amazonaws.com` gives the
  identifier and region, but not the account. A Terraform `identifier` is
  often built from variables: resolve it from the values for that environment.
- A database that runs as a workload in the cluster, such as a `postgres`
  container from a Helm chart, is a Service in telemetry, not a
  `CloudResource`. Its deployment config gives it a `DEPLOYED_FROM` link.
- Use the ARN partition the config shows (`aws`, `aws-cn`, `aws-us-gov`).
- For another kind, use the provider's full id and a short `kind` in the same
  style.

| Property | Value | Required |
|---|---|---|
| `kind` | From the table above | yes |
| `provider` | From the table above | yes |
| `resource_name` | The short name: bucket, queue, subscription, table, topic or database identifier | no |
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
- an SQS, SNS, DynamoDB or RDS ARN without a known account or region
- a reference whose direction is not clear

Resource ids are metadata and may be written. Never write credentials, access
keys, signed URLs, connection strings, or the contents of objects or messages.
