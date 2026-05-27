from datetime import datetime

from pydantic import BaseModel, ConfigDict, EmailStr, Field

MIN_PASSWORD_LENGTH = 5


class UserCreate(BaseModel):
    username: str = Field(min_length=2, max_length=64)
    email: EmailStr
    password: str = Field(min_length=MIN_PASSWORD_LENGTH, max_length=128)


class UserUpdate(BaseModel):
    is_admin: bool | None = None


class UserOut(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    username: str
    email: EmailStr
    is_admin: bool
    created_at: datetime


class ChangePassword(BaseModel):
    current_password: str
    new_password: str = Field(min_length=5, max_length=128)


class Token(BaseModel):
    access_token: str
    token_type: str = "bearer"
    user: UserOut
