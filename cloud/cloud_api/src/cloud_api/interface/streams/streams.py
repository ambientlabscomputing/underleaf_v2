from typing import Annotated

from fastapi import APIRouter, Depends, HTTPException, Query, status

from cloud_api.interface.deps import AccessTokenClaims, get_access_claims
from cloud_api.models.api import (
    ListStreamResponse,
    NewStreamRequest,
    PatchStreamRequest,
    QueryStreamRequest,
    Stream,
)
from cloud_api.service.stream_service import (
    InsufficientNetworkTrafficBalanceError,
    StreamNotFoundError,
    StreamService,
)
from cloud_api.service_manager import get_stream_service

router = APIRouter(prefix="/streams", tags=["Streams"])


async def _require_owned(
    stream_id: str, claims: AccessTokenClaims, service: StreamService
) -> None:
    """404 unless the caller's tenant owns the stream. A foreign stream is
    reported as not-found rather than forbidden so IDs can't be probed."""
    if await service.get_owner_principal_id(stream_id) != claims.azp:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Stream not found"
        )


@router.post(
    "",
    response_model=Stream,
    status_code=status.HTTP_201_CREATED,
    summary="Create a stream",
)
async def create_stream(
    req: NewStreamRequest,
    claims: AccessTokenClaims = Depends(get_access_claims),
    stream_service: StreamService = Depends(get_stream_service),
) -> Stream:
    # The billing account charged is resolved from the connection, so the
    # connection must belong to the caller.
    if (
        await stream_service.get_connection_owner_principal_id(req.connection_id)
        != claims.azp
    ):
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Connection not found"
        )
    try:
        return await stream_service.create_stream(req)
    except InsufficientNetworkTrafficBalanceError:
        raise HTTPException(
            status_code=status.HTTP_402_PAYMENT_REQUIRED,
            detail=(
                "Network traffic allowance exhausted. Upgrade your plan or buy "
                "a data top-up to open new streams."
            ),
        )


@router.get(
    "",
    response_model=ListStreamResponse,
    summary="List streams",
)
async def get_streams(
    query: Annotated[QueryStreamRequest, Query()],
    claims: AccessTokenClaims = Depends(get_access_claims),
    stream_service: StreamService = Depends(get_stream_service),
) -> ListStreamResponse:
    query.principal_account_id = claims.azp
    return await stream_service.get_streams(query)


@router.get(
    "/{stream_id}",
    response_model=Stream,
    summary="Get a stream by ID",
)
async def get_stream(
    stream_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    stream_service: StreamService = Depends(get_stream_service),
) -> Stream:
    await _require_owned(stream_id, claims, stream_service)
    try:
        return await stream_service.get_stream(stream_id)
    except StreamNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Stream not found"
        )


@router.patch(
    "/{stream_id}",
    response_model=Stream,
    summary="Update a stream",
)
async def patch_stream(
    stream_id: str,
    req: PatchStreamRequest,
    claims: AccessTokenClaims = Depends(get_access_claims),
    stream_service: StreamService = Depends(get_stream_service),
) -> Stream:
    await _require_owned(stream_id, claims, stream_service)
    try:
        return await stream_service.patch_stream(stream_id, req)
    except StreamNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Stream not found"
        )


@router.post(
    "/{stream_id}/close",
    response_model=Stream,
    summary="Close a stream",
)
async def close_stream(
    stream_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    stream_service: StreamService = Depends(get_stream_service),
) -> Stream:
    await _require_owned(stream_id, claims, stream_service)
    try:
        return await stream_service.close_stream(stream_id)
    except StreamNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Stream not found"
        )


@router.delete(
    "/{stream_id}",
    status_code=status.HTTP_204_NO_CONTENT,
    summary="Delete a stream",
)
async def delete_stream(
    stream_id: str,
    claims: AccessTokenClaims = Depends(get_access_claims),
    stream_service: StreamService = Depends(get_stream_service),
) -> None:
    await _require_owned(stream_id, claims, stream_service)
    try:
        await stream_service.delete_stream(stream_id)
    except StreamNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Stream not found"
        )
