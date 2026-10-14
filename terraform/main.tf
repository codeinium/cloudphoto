terraform {
  required_version = ">= 1.5.0"
  required_providers {
    yandex = {
      source  = "yandex-cloud/yandex"
      version = ">= 0.175.0, < 1.0.0"
    }
  }
}

# The provider reads YC_TOKEN from the environment.
provider "yandex" {
  folder_id = var.folder_id
}

resource "yandex_iam_service_account" "cloudphoto" {
  folder_id = var.folder_id
  name      = "cloudphoto"
}

resource "yandex_resourcemanager_folder_iam_member" "storage_admin" {
  folder_id = var.folder_id
  role      = "storage.admin"
  member    = "serviceAccount:${yandex_iam_service_account.cloudphoto.id}"
}

resource "yandex_iam_service_account_static_access_key" "cloudphoto" {
  service_account_id = yandex_iam_service_account.cloudphoto.id
  description        = "cloudphoto CLI access to Object Storage"
  depends_on         = [yandex_resourcemanager_folder_iam_member.storage_admin]
}

resource "yandex_storage_bucket" "photos" {
  access_key = yandex_iam_service_account_static_access_key.cloudphoto.access_key
  secret_key = yandex_iam_service_account_static_access_key.cloudphoto.secret_key
  folder_id  = var.folder_id
  bucket     = var.bucket_name
  acl        = "public-read"

  website {
    index_document = "index.html"
    error_document = "error.html"
  }

  https {
    certificate_id = var.cert_id
  }

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "PublicRead"
        Effect    = "Allow"
        Principal = "*"
        Action    = "s3:GetObject"
        Resource  = "arn:aws:s3:::${var.bucket_name}/*"
      },
      {
        Sid       = "ArchiveManagement"
        Effect    = "Allow"
        Principal = { CanonicalUser = yandex_iam_service_account.cloudphoto.id }
        Action    = "s3:*"
        Resource  = ["arn:aws:s3:::${var.bucket_name}", "arn:aws:s3:::${var.bucket_name}/*"]
      },
      {
        Sid       = "CloudConsoleAccess"
        Effect    = "Allow"
        Principal = "*"
        Action    = "s3:*"
        Resource  = ["arn:aws:s3:::${var.bucket_name}", "arn:aws:s3:::${var.bucket_name}/*"]
        Condition = { Bool = { "aws:PrincipalIsAWSService" = "true" } }
      }
    ]
  })

  lifecycle {
    precondition {
      condition     = var.bucket_name == "photos.${trimsuffix(var.domain, ".")}"
      error_message = "bucket_name must equal photos.<domain> for Object Storage custom-domain hosting."
    }
  }
}

resource "yandex_dns_recordset" "photos" {
  zone_id = var.dns_zone_id
  name    = "${var.bucket_name}."
  type    = "CNAME"
  ttl     = 300
  data    = ["${var.bucket_name}.website.yandexcloud.net."]
}
