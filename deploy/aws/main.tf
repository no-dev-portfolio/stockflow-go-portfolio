terraform {
  required_version = ">= 1.13.0, < 2.0.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "6.69.1"
    }
  }
}
provider "aws" { region = var.region }
variable "region" {
  type    = string
  default = "ap-northeast-1"
}
variable "enabled" {
  type        = bool
  default     = false
  description = "Existing private AWS sandbox only. No resources are enabled by default."
}
variable "vpc_id" { type = string }
variable "private_subnet_ids" {
  type = list(string)
  validation {
    condition     = length(var.private_subnet_ids) >= 2
    error_message = "Two private subnets in different availability zones are required."
  }
}
variable "reviewer_cidr" {
  type        = string
  description = "VPN/private-network reviewer CIDR. Never 0.0.0.0/0."
  validation {
    condition     = can(cidrnetmask(var.reviewer_cidr)) && !contains(["0.0.0.0/0", "0.0.0.0/1", "128.0.0.0/1"], var.reviewer_cidr) && try(tonumber(split("/", var.reviewer_cidr)[1]) >= 16, false)
    error_message = "Use an explicit restricted IPv4 CIDR (/16 or narrower)."
  }
}
variable "image_uri" {
  type        = string
  description = "Image already pushed to ECR, pinned by digest."
  validation {
    condition     = can(regex("@sha256:[a-f0-9]{64}$", var.image_uri))
    error_message = "The container image must be pinned by sha256 digest."
  }
}
variable "database_url_secret_arn" {
  type        = string
  description = "Existing Secrets Manager secret containing a PostgreSQL connection URL."
}
variable "database_security_group_id" {
  type        = string
  description = "Security group of an existing private PostgreSQL/RDS sandbox database."
}
resource "aws_cloudwatch_log_group" "app" {
  count             = var.enabled ? 1 : 0
  name              = "/stockflow/sandbox"
  retention_in_days = 7
}
resource "aws_ecs_cluster" "app" {
  count = var.enabled ? 1 : 0
  name  = "stockflow-sandbox"
}
resource "aws_iam_role" "execution" {
  count              = var.enabled ? 1 : 0
  name               = "stockflow-sandbox-execution"
  assume_role_policy = jsonencode({ Version = "2012-10-17", Statement = [{ Effect = "Allow", Principal = { Service = "ecs-tasks.amazonaws.com" }, Action = "sts:AssumeRole" }] })
}
resource "aws_iam_role_policy_attachment" "execution" {
  count      = var.enabled ? 1 : 0
  role       = aws_iam_role.execution[0].name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}
resource "aws_iam_role_policy" "secret" {
  count  = var.enabled ? 1 : 0
  role   = aws_iam_role.execution[0].id
  policy = jsonencode({ Version = "2012-10-17", Statement = [{ Effect = "Allow", Action = ["secretsmanager:GetSecretValue"], Resource = var.database_url_secret_arn }] })
}
resource "aws_security_group" "alb" {
  count  = var.enabled ? 1 : 0
  name   = "stockflow-sandbox-alb"
  vpc_id = var.vpc_id
}
resource "aws_security_group" "task" {
  count  = var.enabled ? 1 : 0
  name   = "stockflow-sandbox-task"
  vpc_id = var.vpc_id
}
resource "aws_vpc_security_group_ingress_rule" "reviewer" {
  count             = var.enabled ? 1 : 0
  security_group_id = aws_security_group.alb[0].id
  cidr_ipv4         = var.reviewer_cidr
  ip_protocol       = "tcp"
  from_port         = 80
  to_port           = 80
}
resource "aws_vpc_security_group_egress_rule" "alb_to_task" {
  count                        = var.enabled ? 1 : 0
  security_group_id            = aws_security_group.alb[0].id
  referenced_security_group_id = aws_security_group.task[0].id
  ip_protocol                  = "tcp"
  from_port                    = 8080
  to_port                      = 8080
}
resource "aws_vpc_security_group_ingress_rule" "task_from_alb" {
  count                        = var.enabled ? 1 : 0
  security_group_id            = aws_security_group.task[0].id
  referenced_security_group_id = aws_security_group.alb[0].id
  ip_protocol                  = "tcp"
  from_port                    = 8080
  to_port                      = 8080
}
resource "aws_vpc_security_group_egress_rule" "task_database" {
  count                        = var.enabled ? 1 : 0
  security_group_id            = aws_security_group.task[0].id
  referenced_security_group_id = var.database_security_group_id
  ip_protocol                  = "tcp"
  from_port                    = 5432
  to_port                      = 5432
}
resource "aws_vpc_security_group_ingress_rule" "database_from_task" {
  count                        = var.enabled ? 1 : 0
  security_group_id            = var.database_security_group_id
  referenced_security_group_id = aws_security_group.task[0].id
  ip_protocol                  = "tcp"
  from_port                    = 5432
  to_port                      = 5432
}
resource "aws_vpc_security_group_egress_rule" "aws_services" {
  count             = var.enabled ? 1 : 0
  security_group_id = aws_security_group.task[0].id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
  description       = "HTTPS for existing NAT or private endpoints. Restrict further in the target VPC."
}
resource "aws_lb" "app" {
  count              = var.enabled ? 1 : 0
  name               = "stockflow-sandbox"
  internal           = true
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb[0].id]
  subnets            = var.private_subnet_ids
}
resource "aws_lb_target_group" "app" {
  count       = var.enabled ? 1 : 0
  name        = "stockflow-sandbox"
  port        = 8080
  protocol    = "HTTP"
  target_type = "ip"
  vpc_id      = var.vpc_id
  health_check { path = "/readyz" }
}
resource "aws_lb_listener" "app" {
  count             = var.enabled ? 1 : 0
  load_balancer_arn = aws_lb.app[0].arn
  port              = 80
  protocol          = "HTTP"
  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.app[0].arn
  }
}
resource "aws_ecs_task_definition" "app" {
  count                    = var.enabled ? 1 : 0
  family                   = "stockflow-sandbox"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = "256"
  memory                   = "512"
  execution_role_arn       = aws_iam_role.execution[0].arn
  runtime_platform {
    cpu_architecture        = "ARM64"
    operating_system_family = "LINUX"
  }
  container_definitions = jsonencode([{ name = "stockflow", image = var.image_uri, essential = true, readonlyRootFilesystem = true, user = "10001:10001", portMappings = [{ containerPort = 8080 }], environment = [{ name = "LISTEN_ADDR", value = "0.0.0.0:8080" }, { name = "DEMO_MODE", value = "true" }], secrets = [{ name = "DATABASE_URL", valueFrom = var.database_url_secret_arn }], logConfiguration = { logDriver = "awslogs", options = { awslogs-group = aws_cloudwatch_log_group.app[0].name, awslogs-region = var.region, awslogs-stream-prefix = "app" } } }])
}
resource "aws_ecs_service" "app" {
  count           = var.enabled ? 1 : 0
  name            = "stockflow-sandbox"
  cluster         = aws_ecs_cluster.app[0].id
  task_definition = aws_ecs_task_definition.app[0].arn
  desired_count   = 1
  launch_type     = "FARGATE"
  network_configuration {
    subnets          = var.private_subnet_ids
    security_groups  = [aws_security_group.task[0].id]
    assign_public_ip = false
  }
  load_balancer {
    target_group_arn = aws_lb_target_group.app[0].arn
    container_name   = "stockflow"
    container_port   = 8080
  }
  depends_on = [aws_lb_listener.app, aws_iam_role_policy_attachment.execution, aws_iam_role_policy.secret]
}
output "private_review_endpoint" {
  value = var.enabled ? "http://${aws_lb.app[0].dns_name}" : null
}
