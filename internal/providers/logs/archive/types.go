package archive

const (
	TypeS3    = "s3"
	TypeGCS   = "gcs"
	TypeAzure = "azure"
)

// The archive accepts only the logs signal.
const streamSignal = "logs"

type CellDetails struct {
	CloudProvider string           `json:"cloud_provider"`
	Region        string           `json:"region"`
	AWS           CellDetailsAWS   `json:"aws"`
	GCP           CellDetailsGCP   `json:"gcp"`
	Azure         CellDetailsAzure `json:"azure"`
}

type CellDetailsAWS struct {
	ARN string `json:"arn"`
}

type CellDetailsGCP struct {
	ServiceAccount string `json:"service_account"`
}

type CellDetailsAzure struct {
	ApplicationID string `json:"application_id"`
}

func (c CellDetails) Principal() string {
	switch c.CloudProvider {
	case "aws":
		return c.AWS.ARN
	case "gcp":
		return c.GCP.ServiceAccount
	case "azure":
		return c.Azure.ApplicationID
	default:
		return ""
	}
}

func (c CellDetails) DestinationType() string {
	switch c.CloudProvider {
	case "aws":
		return TypeS3
	case "gcp":
		return TypeGCS
	case "azure":
		return TypeAzure
	default:
		return ""
	}
}

type StorageConfig struct {
	S3    *S3Config    `json:"s3,omitempty"`
	GCS   *GCSConfig   `json:"gcs,omitempty"`
	Azure *AzureConfig `json:"azure,omitempty"`
}

type S3Config struct {
	BucketName string `json:"bucket_name"`
	Region     string `json:"region"`
	PathPrefix string `json:"path_prefix,omitempty"`
}

type GCSConfig struct {
	BucketName string `json:"bucket_name"`
	PathPrefix string `json:"path_prefix,omitempty"`
}

type AzureConfig struct {
	StorageAccount string `json:"storage_account"`
	Container      string `json:"container"`
	PathPrefix     string `json:"path_prefix,omitempty"`
	TenantID       string `json:"tenant_id"`
}

type Destination struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Description   string        `json:"description"`
	Type          string        `json:"type"`
	StorageConfig StorageConfig `json:"storage_config"`
	CreatedAt     string        `json:"created_at"`
	UpdatedAt     string        `json:"updated_at"`
}

type DestinationCreate struct {
	Name          string        `json:"name"`
	Description   string        `json:"description,omitempty"`
	Type          string        `json:"type"`
	StorageConfig StorageConfig `json:"storage_config"`
}

type Stream struct {
	ID                   string `json:"id"`
	ArchiveDestinationID string `json:"archive_destination_id"`
	Name                 string `json:"name"`
	Signal               string `json:"signal"`
	StreamSelector       string `json:"stream_selector"`
	CreatedAt            string `json:"created_at"`
	UpdatedAt            string `json:"updated_at"`
}

type StreamCreate struct {
	ArchiveDestinationID string `json:"archive_destination_id"`
	Name                 string `json:"name"`
	Signal               string `json:"signal"`
	StreamSelector       string `json:"stream_selector"`
}

// The API replaces the whole stream, so every field must be set; the signal
// cannot change.
type StreamUpdate struct {
	ArchiveDestinationID string `json:"archive_destination_id"`
	Name                 string `json:"name"`
	StreamSelector       string `json:"stream_selector"`
}

type StreamRow struct {
	Stream

	ArchiveDestinationName string `json:"archive_destination_name,omitempty"`
}
