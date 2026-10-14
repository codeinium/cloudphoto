output "site_url" {
  value = "https://${var.bucket_name}/"
}

output "bucket_name" {
  value = yandex_storage_bucket.photos.bucket
}

output "aws_access_key_id" {
  value     = yandex_iam_service_account_static_access_key.cloudphoto.access_key
  sensitive = true
}

output "aws_secret_access_key" {
  value     = yandex_iam_service_account_static_access_key.cloudphoto.secret_key
  sensitive = true
}
