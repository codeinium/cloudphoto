variable "folder_id" {
  type        = string
  description = "Yandex Cloud folder ID"
}

variable "bucket_name" {
  type        = string
  description = "Bucket name, equal to photos.<domain> and the CLI BUCKET value"
}

variable "domain" {
  type        = string
  description = "Existing DNS zone domain, for example vvot01.itiscl.ru"
}

variable "cert_id" {
  type        = string
  description = "Existing issued Certificate Manager certificate ID"
}

variable "dns_zone_id" {
  type        = string
  description = "ID of the existing Cloud DNS zone, not its domain name"
}
