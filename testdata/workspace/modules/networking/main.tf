resource "aws_subnet" "public" {
  cidr_block = var.cidr_block    # <-- var.cidr_block here = aws_vpc.main.id in root
}