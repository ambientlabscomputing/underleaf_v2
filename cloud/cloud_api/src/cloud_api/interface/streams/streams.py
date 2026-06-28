from fastapi import APIRouter, Depends, HTTPException, status, Query
from typing import Annotated

from cloud_api.interface.deps import AccessTokenClaims, get_access_claims
from cloud_api.models.api import (
    QueryConnectionRequest,
    Stream,
    ListStreamResponse,
    PatchStreamRequest,
    QueryStreamRequest,
    NewStreamRequest,
)
from cloud_api.service.stream_service import StreamNotFoundError, StreamService
from cloud_api.service_manager import get_stream_service

router = APIRouter(prefix="/streams", tags=["Streams"])


@router.post(
    "",
    response_model=Stream,
    status_code=status.HTTP_201_CREATED,
    summary="Create a stream",
)
async def create_stream(
    req: NewStreamRequest,
    _: AccessTokenClaims = Depends(get_access_claims),
    stream_service: StreamService = Depends(get_stream_service),
) -> Stream:
    return await stream_service.create_stream(req)


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
    _: AccessTokenClaims = Depends(get_access_claims),
    stream_service: StreamService = Depends(get_stream_service),
) -> Stream:
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
    try:
        await stream_service.delete_stream(stream_id)
    except StreamNotFoundError:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Stream not found"
        )
