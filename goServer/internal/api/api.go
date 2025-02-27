package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"server/internal/api/spec"
	"server/internal/pgstore"
	"strings"
	"time"

	openapi_types "github.com/discord-gophers/goapi-gen/types"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type store interface {
	GetTrip(ctx context.Context, id uuid.UUID) (pgstore.Trip, error)
	UpdateTrip(ctx context.Context, arg pgstore.UpdateTripParams) error
	CreateTrip(ctx context.Context, pool *pgxpool.Pool, params spec.CreateTripRequest) (uuid.UUID, error)
	GetParticipant(ctx context.Context, participantID uuid.UUID) (pgstore.Participant, error)
	ConfirmParticipant(ctx context.Context, participantID uuid.UUID) error
	GetTripActivities(ctx context.Context, tripID uuid.UUID) ([]pgstore.Activity, error)
	CreateActivity(ctx context.Context, arg pgstore.CreateActivityParams) (uuid.UUID, error)
	GetParticipants(ctx context.Context, tripID uuid.UUID) ([]pgstore.Participant, error)
	InviteParticipantToTrip(ctx context.Context, params pgstore.InviteParticipantsToTripParams) (uuid.UUID, error)
	GetTripLinks(ctx context.Context, tripID uuid.UUID) ([]pgstore.Link, error)
	CreateTripLink(ctx context.Context, arg pgstore.CreateTripLinkParams) (uuid.UUID, error)
}

type mailer interface {
	SendTripConfirmationEmail(tripID uuid.UUID) error
	SendTripConfirmedEmail(tripID, participantID uuid.UUID) error
}

type API struct {
	store     store
	logger    *zap.Logger
	validator *validator.Validate
	pool      *pgxpool.Pool
	mailer    mailer
}

func NewAPI(pool *pgxpool.Pool, logger *zap.Logger, mailer mailer) API {
	validator := validator.New(validator.WithRequiredStructEnabled())
	return API{pgstore.New(pool), logger, validator, pool, mailer}
}

// Confirms a participant on a trip.
// (PATCH /participants/{participantId}/confirm)
func (api API) PatchParticipantsParticipantIDConfirm(w http.ResponseWriter, r *http.Request, participantID string) *spec.Response {
	id, err := uuid.Parse(participantID)
	if err != nil {
		return spec.PatchParticipantsParticipantIDConfirmJSON400Response(spec.Error{Message: "uuid invalido"})
	}

	participant, err := api.store.GetParticipant(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return spec.PatchParticipantsParticipantIDConfirmJSON400Response(spec.Error{Message: "Participante não encontrado"})
		}
		api.logger.Error("Failed to get participant", zap.Error(err), zap.String("participant_id", participantID))
		return spec.PatchParticipantsParticipantIDConfirmJSON400Response(
			spec.Error{Message: "Something went wrong, try again later"},
		)

	}

	if participant.IsConfirmed {
		return spec.PatchParticipantsParticipantIDConfirmJSON400Response(
			spec.Error{Message: "Participant already confirmed"},
		)
	}

	if err := api.store.ConfirmParticipant(r.Context(), id); err != nil {
		api.logger.Error("Failed to confirm participant", zap.Error(err), zap.String("participant_id", participantID))

		return spec.PatchParticipantsParticipantIDConfirmJSON400Response(
			spec.Error{Message: "Something went wrong, try again later"},
		)
	}

	return spec.PatchParticipantsParticipantIDConfirmJSON204Response(nil)
}

// Create a new trip
// (POST /trips)
func (api API) PostTrips(w http.ResponseWriter, r *http.Request) *spec.Response {
	var body spec.CreateTripRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return spec.PostTripsJSON400Response(spec.Error{Message: "invalid JSON: " + err.Error()})
	}

	if err := api.validator.Struct((body)); err != nil {
		return spec.PostTripsJSON400Response(spec.Error{Message: "invalid input: " + err.Error()})
	}

	tripID, err := api.store.CreateTrip(r.Context(), api.pool, body)
	if err != nil {
		return spec.PostTripsJSON400Response(spec.Error{Message: "Failed to create trip, try again later: " + err.Error()})

	}

	go func() {
		if err := api.mailer.SendTripConfirmationEmail(tripID); err != nil {
			api.logger.Error("failed to send email on PostTrips", zap.Error(err), zap.String("trip_id", tripID.String()))
		}
	}()

	return spec.PostTripsJSON201Response(spec.CreateTripResponse{TripID: tripID.String()})
}

// Get a trip details.
// (GET /trips/{tripId})
func (api API) GetTripsTripID(w http.ResponseWriter, r *http.Request, tripID string) *spec.Response {
	id, err := uuid.Parse(tripID)
	if err != nil {
		return spec.GetTripsTripIDJSON400Response(spec.Error{Message: "uuid invalido"})
	}
	trip, err := api.store.GetTrip(r.Context(), id)
	if err != nil {
		return spec.GetTripsTripIDJSON400Response(spec.Error{Message: "Failed to get trip"})
	}

	response := spec.GetTripDetailsResponse{
		Trip: spec.GetTripDetailsResponseTripObj{
			ID:          trip.ID.String(),
			Destination: trip.Destination,
			EndsAt:      trip.EndsAt.Time,
			IsConfirmed: trip.IsConfirmed,
			StartsAt:    trip.StartsAt.Time,
		},
	}

	return spec.GetTripsTripIDJSON200Response(response)
}

// Update a trip.
// (PUT /trips/{tripId})
func (api API) PutTripsTripID(w http.ResponseWriter, r *http.Request, tripID string) *spec.Response {
	var body spec.UpdateTripRequest

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return spec.PutTripsTripIDJSON400Response(spec.Error{Message: "invalid JSON: " + err.Error()})
	}

	if err := api.validator.Struct((body)); err != nil {
		return spec.PutTripsTripIDJSON400Response(spec.Error{Message: "invalid input: " + err.Error()})
	}

	id, err := uuid.Parse(tripID)
	if err != nil {
		return spec.PutTripsTripIDJSON400Response(spec.Error{Message: "invalid uuid"})
	}

	trip, err := api.store.GetTrip(r.Context(), id)
	if err != nil {
		return spec.GetTripsTripIDJSON400Response(spec.Error{Message: "Failed to get trip"})
	}

	newData := pgstore.UpdateTripParams{
		Destination: body.Destination,
		StartsAt:    pgtype.Timestamp{Valid: true, Time: body.StartsAt},
		EndsAt:      pgtype.Timestamp{Valid: true, Time: body.EndsAt},
		IsConfirmed: trip.IsConfirmed,
		ID:          id,
	}

	if err := api.store.UpdateTrip(r.Context(), newData); err != nil {
		return spec.PutTripsTripIDJSON400Response(spec.Error{Message: "Failed to update trip"})
	}

	return spec.PutTripsTripIDJSON204Response(nil)
}

// Get a trip activities.
// (GET /trips/{tripId}/activities)
func (api API) GetTripsTripIDActivities(w http.ResponseWriter, r *http.Request, tripID string) *spec.Response {
	id, err := uuid.Parse(tripID)
	if err != nil {
		return spec.GetTripsTripIDActivitiesJSON400Response(spec.Error{Message: "uuid invalido"})
	}

	activities, err := api.store.GetTripActivities(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return spec.GetTripsTripIDActivitiesJSON400Response(spec.Error{Message: "trip not found"})
		}
		api.logger.Error("failed to find trip activities", zap.Error(err), zap.String("trip_id", tripID))
		return spec.GetTripsTripIDActivitiesJSON400Response(spec.Error{Message: "something went wrong, try again"})
	}

	var output spec.GetTripActivitiesResponse

	activityMap := make(map[string][]pgstore.Activity)

	for _, act := range activities {
		date := act.OccursAt.Time.Format(time.DateOnly)
		activityMap[date] = append(activityMap[date], act)
	}

	for dateStr, actsOnDate := range activityMap {
		var innerActs []spec.GetTripActivitiesResponseInnerArray

		for _, act := range actsOnDate {
			innerActs = append(innerActs, spec.GetTripActivitiesResponseInnerArray{
				ID:       act.ID.String(),
				OccursAt: act.OccursAt.Time,
				Title:    act.Title,
			})
		}

		date, _ := time.Parse(time.DateOnly, dateStr)
		output.Activities = append(output.Activities, spec.GetTripActivitiesResponseOuterArray{
			Date:       date,
			Activities: innerActs,
		})
	}

	return spec.GetTripsTripIDActivitiesJSON200Response(output)
}

// Create a trip activity.
// (POST /trips/{tripId}/activities)
func (api API) PostTripsTripIDActivities(w http.ResponseWriter, r *http.Request, tripID string) *spec.Response {
	id, err := uuid.Parse(tripID)
	if err != nil {
		return spec.GetTripsTripIDActivitiesJSON400Response(spec.Error{Message: "invalid uuid"})
	}

	var body spec.CreateActivityRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return spec.PostTripsTripIDActivitiesJSON400Response(spec.Error{Message: "invalid JSON: " + err.Error()})
	}

	if err := api.validator.Struct((body)); err != nil {
		return spec.PostTripsTripIDActivitiesJSON400Response(spec.Error{Message: "invalid input: " + err.Error()})
	}

	activityID, err := api.store.CreateActivity(r.Context(), pgstore.CreateActivityParams{
		TripID:   id,
		Title:    body.Title,
		OccursAt: pgtype.Timestamp{Valid: true, Time: body.OccursAt},
	})
	if err != nil {
		return spec.PostTripsTripIDActivitiesJSON400Response(spec.Error{Message: "Failed to create activity, try again later"})
	}

	return spec.PostTripsTripIDActivitiesJSON201Response(spec.CreateActivityResponse{ActivityID: activityID.String()})
}

// Confirm a trip and send e-mail invitations.
// (GET /trips/{tripId}/confirm)
func (api API) GetTripsTripIDConfirm(w http.ResponseWriter, r *http.Request, tripID string) *spec.Response {
	id, err := uuid.Parse(tripID)
	if err != nil {
		return spec.GetTripsTripIDConfirmJSON400Response(spec.Error{Message: "uuid invalido"})
	}

	trip, err := api.store.GetTrip(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return spec.GetTripsTripIDConfirmJSON400Response(spec.Error{Message: "Trip not found"})
		}
		api.logger.Error("Failed to get trip", zap.Error(err), zap.String("trip_id", tripID))
		return spec.GetTripsTripIDConfirmJSON400Response(
			spec.Error{Message: "Something went wrong, try again later"},
		)

	}

	if trip.IsConfirmed {
		return spec.GetTripsTripIDConfirmJSON400Response(
			spec.Error{Message: "Trip already confirmed"},
		)
	}

	if err := api.store.UpdateTrip(r.Context(), pgstore.UpdateTripParams{
		Destination: trip.Destination,
		EndsAt:      trip.EndsAt,
		StartsAt:    trip.StartsAt,
		IsConfirmed: true,
		ID:          id,
	}); err != nil {
		api.logger.Error("failed to update trip", zap.Error(err), zap.String("trip_id", tripID))
		return spec.GetTripsTripIDConfirmJSON400Response(spec.Error{Message: "something went wrong, try again"})
	}

	return spec.GetTripsTripIDConfirmJSON204Response(nil)
}

// Invite someone to the trip.
// (POST /trips/{tripId}/invites)
func (api API) PostTripsTripIDInvites(w http.ResponseWriter, r *http.Request, tripID string) *spec.Response {
	id, err := uuid.Parse(tripID)
	if err != nil {
		return spec.PostTripsTripIDInvitesJSON400Response(spec.Error{Message: "invalid uuid passed: " + err.Error()})
	}

	var body spec.PostTripsTripIDInvitesJSONBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return spec.PostTripsTripIDInvitesJSON400Response(spec.Error{Message: err.Error()})
	}

	participantID, err := api.store.InviteParticipantToTrip(r.Context(), pgstore.InviteParticipantsToTripParams{
		TripID: id,
		Email:  string(body.Email),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return spec.PostTripsTripIDInvitesJSON400Response(spec.Error{Message: "trip not found"})
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == pgerrcode.UniqueViolation {
				return spec.PostTripsTripIDInvitesJSON400Response(spec.Error{Message: "participant already invited"})
			}
		}
		api.logger.Error(
			"failed to invite participant to trip",
			zap.Error(err),
			zap.String("trip_id", tripID),
			zap.String("participant_email", string(body.Email)),
		)
		return spec.PostTripsTripIDInvitesJSON400Response(spec.Error{Message: "something went wrong, try again"})
	}

	go func() {
		if err := api.mailer.SendTripConfirmedEmail(id, participantID); err != nil {
			api.logger.Error(
				"failed to send trip confirmed email",
				zap.Error(err),
				zap.String("participant_id", participantID.String()),
				zap.String("trip_id", tripID),
			)
		}
	}()

	return spec.PostTripsTripIDInvitesJSON201Response(nil)
}

// Get a trip links.
// (GET /trips/{tripId}/links)
func (api API) GetTripsTripIDLinks(w http.ResponseWriter, r *http.Request, tripID string) *spec.Response {
	id, err := uuid.Parse(tripID)
	if err != nil {
		return spec.GetTripsTripIDLinksJSON400Response(spec.Error{Message: "invalid uuid passed: " + err.Error()})
	}

	links, err := api.store.GetTripLinks(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return spec.GetTripsTripIDLinksJSON400Response(spec.Error{Message: "trip not found"})
		}
		api.logger.Error("failed to find trip links", zap.Error(err), zap.String("trip_id", tripID))
		return spec.GetTripsTripIDLinksJSON400Response(spec.Error{Message: "something went wrong, try again"})
	}

	var output spec.GetLinksResponse

	for _, link := range links {
		output.Links = append(output.Links, spec.GetLinksResponseArray{
			ID:    link.ID.String(),
			Title: link.Title,
			URL:   link.Url,
		})
	}

	return spec.GetTripsTripIDLinksJSON200Response(output)
}

// Create a trip link.
// (POST /trips/{tripId}/links)
func (api API) PostTripsTripIDLinks(w http.ResponseWriter, r *http.Request, tripID string) *spec.Response {
	id, err := uuid.Parse(tripID)
	if err != nil {
		return spec.PostTripsTripIDLinksJSON400Response(spec.Error{Message: "invalid uuid passed: " + err.Error()})
	}

	var body spec.PostTripsTripIDLinksJSONBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return spec.PostTripsTripIDLinksJSON400Response(spec.Error{Message: err.Error()})
	}

	linkID, err := api.store.CreateTripLink(r.Context(), pgstore.CreateTripLinkParams{
		TripID: id,
		Title:  body.Title,
		Url:    body.URL,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return spec.PostTripsTripIDLinksJSON400Response(spec.Error{Message: "trip not found"})
		}
		api.logger.Error("failed to find trip participants", zap.Error(err), zap.String("trip_id", tripID))
		return spec.PostTripsTripIDActivitiesJSON400Response(spec.Error{Message: "something went wrong, try again"})
	}

	return spec.PostTripsTripIDLinksJSON201Response(spec.CreateLinkResponse{LinkID: linkID.String()})

}

// Get a trip participants.
// (GET /trips/{tripId}/participants)
func (api API) GetTripsTripIDParticipants(w http.ResponseWriter, r *http.Request, tripID string) *spec.Response {
	id, err := uuid.Parse(tripID)
	if err != nil {
		return spec.GetTripsTripIDParticipantsJSON400Response(
			spec.Error{Message: "invalid uuid passed: " + err.Error()},
		)
	}

	participants, err := api.store.GetParticipants(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return spec.GetTripsTripIDParticipantsJSON400Response(spec.Error{Message: "trip not found"})
		}
		api.logger.Error("failed to find trip participants", zap.Error(err), zap.String("trip_id", tripID))
		return spec.GetTripsTripIDParticipantsJSON400Response(spec.Error{Message: "something went wrong, try again"})
	}

	var output spec.GetTripParticipantsResponse

	output.Participants = make([]spec.GetTripParticipantsResponseArray, len(participants))

	for i, p := range participants {
		var name string
		parsedEmail, err := mail.ParseAddress(p.Email)
		if err == nil {
			addr := parsedEmail.Address
			name = addr[:strings.Index(addr, "@")]
		}
		output.Participants[i] = spec.GetTripParticipantsResponseArray{
			Email:       openapi_types.Email(p.Email),
			ID:          p.ID.String(),
			IsConfirmed: p.IsConfirmed,
			Name:        &name,
		}
	}

	return spec.GetTripsTripIDParticipantsJSON200Response(output)
}
