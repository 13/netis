from fastapi import APIRouter

from . import admin, auth, devices, discovery, exports, ip_addresses, subnets, users

api_router = APIRouter(prefix="/api")
api_router.include_router(auth.router)
api_router.include_router(users.router)
api_router.include_router(subnets.router)
api_router.include_router(devices.router)
api_router.include_router(ip_addresses.router)
api_router.include_router(discovery.router)
api_router.include_router(exports.router)
api_router.include_router(admin.router)

__all__ = ["api_router"]
